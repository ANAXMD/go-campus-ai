package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/cors"
)

// Cache keys embed a version number (the "generation") that every write bumps.
// Bumping it makes all previously cached pages unreachable at once, so there is
// no need to find and delete them; they simply expire.
//
// This is safer than deleting keys: with delete-on-write, a slow reader can
// fetch old rows, lose a race with a writer, and then store the old rows in
// the cache after the delete, leaving a stale entry. Under versioning that
// reader writes to the old generation's key, which nobody reads any more.
//
// It assumes the counter key is not evicted on its own. Redis only evicts when
// maxmemory is set, which the default configuration does not do.
const (
	cacheGenKey  = "students:gen"
	defaultLimit = 25
	maxLimit     = 100
	maxSearchLen = 100
)

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
	mux.HandleFunc("GET /api/students/stats", s.RequireJWT(s.handleStudentStats))
	mux.HandleFunc("POST /api/students", s.RequireRole(RoleAdmin, s.handleCreateStudent))
	mux.HandleFunc("PUT /api/students/{id}", s.RequireRole(RoleAdmin, s.handleUpdateStudent))
	mux.HandleFunc("DELETE /api/students/{id}", s.RequireRole(RoleAdmin, s.handleDeleteStudent))

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

// cacheKey builds a key under the current generation. If Redis cannot be
// read, the generation falls back to "0"; the writes that would have used that
// key fail too, so nothing wrong is ever stored.
func (s *Server) cacheKey(ctx context.Context, parts ...string) string {
	gen := "0"
	if raw, ok := s.cache.Get(ctx, cacheGenKey); ok {
		gen = string(raw)
	}
	return "students:v" + gen + ":" + strings.Join(parts, ":")
}

// serveCached is the cache-aside read shared by the list and stats endpoints:
// answer from Redis when possible, otherwise compute, store and answer.
func (s *Server) serveCached(w http.ResponseWriter, r *http.Request, key, what string, compute func(ctx context.Context) (any, error)) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")

	if cached, ok := s.cache.Get(ctx, key); ok {
		w.Header().Set("X-Cache", "HIT")
		_, _ = w.Write(cached)
		return
	}

	v, err := compute(ctx)
	if err != nil {
		s.internalError(w, what, err)
		return
	}
	body, err := jsonBytes(v)
	if err != nil {
		s.internalError(w, "encode "+what, err)
		return
	}
	if err := s.cache.Set(ctx, key, body, s.cfg.CacheTTL); err != nil {
		s.log.Printf("cache set failed (serving from database): %v", err)
	}
	w.Header().Set("X-Cache", "MISS")
	_, _ = w.Write(body)
}

// parseStudentQuery reads ?q=&limit=&offset= and rejects anything out of range
// instead of silently clamping it, so a client bug shows up as a 400.
func parseStudentQuery(r *http.Request) (StudentQuery, error) {
	v := r.URL.Query()
	q := StudentQuery{Search: strings.TrimSpace(v.Get("q")), Limit: defaultLimit}

	if len(q.Search) > maxSearchLen {
		return q, fmt.Errorf("q must be at most %d characters", maxSearchLen)
	}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			return q, fmt.Errorf("limit must be a whole number from 1 to %d", maxLimit)
		}
		q.Limit = n
	}
	if raw := v.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return q, errors.New("offset must be a whole number, 0 or more")
		}
		q.Offset = n
	}
	return q, nil
}

// handleListStudents returns one page of the directory. Pages are cached under
// a key that includes the search text, limit and offset (and the generation,
// see above), so every distinct page is cached on its own.
func (s *Server) handleListStudents(w http.ResponseWriter, r *http.Request) {
	q, err := parseStudentQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := s.cacheKey(r.Context(), "list", strconv.Itoa(q.Limit), strconv.Itoa(q.Offset), strings.ToLower(q.Search))
	s.serveCached(w, r, key, "list students", func(ctx context.Context) (any, error) {
		page, err := s.store.ListStudents(ctx, q)
		if page.Items == nil {
			page.Items = []Student{} // encode an empty page as [] rather than null
		}
		return page, err
	})
}

func (s *Server) handleStudentStats(w http.ResponseWriter, r *http.Request) {
	s.serveCached(w, r, s.cacheKey(r.Context(), "stats"), "student stats", func(ctx context.Context) (any, error) {
		return s.store.StudentStats(ctx)
	})
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

// invalidateStudents bumps the cache generation after a successful write. If
// Redis is down the stale pages still expire after CacheTTL, so the failure is
// bounded.
func (s *Server) invalidateStudents(ctx context.Context) {
	if _, err := s.cache.Incr(ctx, cacheGenKey); err != nil {
		s.log.Printf("cache invalidation failed (lists may be stale for up to %v): %v", s.cfg.CacheTTL, err)
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
