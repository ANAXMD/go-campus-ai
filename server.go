package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/cors"
)

const studentsCacheKey = "students:all"

// Server wires the HTTP handlers to their dependencies.
type Server struct {
	cfg       Config
	store     Store
	cache     Cache
	log       *log.Logger
	dummyHash []byte
	now       func() time.Time
	notify    func(Student) // called in the background after a student is created
	wg        sync.WaitGroup
}

func NewServer(cfg Config, store Store, cache Cache, logger *log.Logger) (*Server, error) {
	dummy, err := hashForTiming()
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, store: store, cache: cache, log: logger, dummyHash: dummy, now: time.Now}
	s.notify = s.sendWelcomeEmail
	return s, nil
}

// Handler returns the full HTTP handler: CORS, request logging, routing.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/register", s.handleRegister)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/students", s.RequireJWT(s.handleListStudents))
	mux.HandleFunc("POST /api/students", s.RequireJWT(s.handleCreateStudent))
	mux.HandleFunc("PUT /api/students/{id}", s.RequireJWT(s.handleUpdateStudent))
	mux.HandleFunc("DELETE /api/students/{id}", s.RequireJWT(s.handleDeleteStudent))

	return cors.New(cors.Options{
		AllowedOrigins: s.cfg.AllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:         600,
	}).Handler(s.requestLogger(mux))
}

// Wait blocks until background work (welcome emails) has finished. It is
// called during graceful shutdown so in-flight emails are not cut off.
func (s *Server) Wait() { s.wg.Wait() }

func (s *Server) internalError(w http.ResponseWriter, what string, err error) {
	s.log.Printf("ERROR %s: %v", what, err) // the detail stays in the log, never in the response
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.log.Printf("health: database unreachable: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleListStudents is a cache-aside read: serve from Redis when possible,
// otherwise read Postgres and populate the cache. Every write below deletes
// the key, so a cached list is never older than the most recent write.
func (s *Server) handleListStudents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")

	if cached, ok := s.cache.Get(ctx, studentsCacheKey); ok {
		w.Header().Set("X-Cache", "HIT")
		_, _ = w.Write(cached)
		return
	}

	students, err := s.store.ListStudents(ctx)
	if err != nil {
		s.internalError(w, "list students", err)
		return
	}
	if students == nil {
		students = []Student{} // encode an empty table as [] rather than null
	}
	body, err := jsonBytes(students)
	if err != nil {
		s.internalError(w, "encode students", err)
		return
	}
	if err := s.cache.Set(ctx, studentsCacheKey, body, s.cfg.CacheTTL); err != nil {
		s.log.Printf("cache set failed (serving from database): %v", err)
	}
	w.Header().Set("X-Cache", "MISS")
	_, _ = w.Write(body)
}

func (s *Server) handleCreateStudent(w http.ResponseWriter, r *http.Request) {
	var in Student
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateStudent(&in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := s.store.CreateStudent(r.Context(), in)
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusConflict, "a student with this email already exists")
		return
	}
	if err != nil {
		s.internalError(w, "create student", err)
		return
	}
	s.invalidateStudents(r.Context())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.notify(created)
	}()

	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateStudent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in Student
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateStudent(&in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := s.store.UpdateStudent(r.Context(), id, in)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "student not found")
		return
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "a student with this email already exists")
		return
	case err != nil:
		s.internalError(w, "update student", err)
		return
	}
	s.invalidateStudents(r.Context())
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteStudent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteStudent(r.Context(), id)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "student not found")
		return
	case err != nil:
		s.internalError(w, "delete student", err)
		return
	}
	s.invalidateStudents(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// invalidateStudents drops the cached list after a write. If Redis is down
// the stale entry still expires after CacheTTL, so the failure is bounded.
func (s *Server) invalidateStudents(ctx context.Context) {
	if err := s.cache.Delete(ctx, studentsCacheKey); err != nil {
		s.log.Printf("cache invalidation failed (list may be stale for up to %v): %v", s.cfg.CacheTTL, err)
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "student id must be a positive integer")
		return 0, false
	}
	return id, true
}

// sendWelcomeEmail stands in for a call to an email provider such as SES or
// SendGrid: slow, external, and not something the HTTP response should wait on.
func (s *Server) sendWelcomeEmail(st Student) {
	s.log.Printf("email: sending welcome message to student %d", st.ID)
	time.Sleep(s.cfg.EmailDelay)
	s.log.Printf("email: delivered to student %d", st.ID)
}
