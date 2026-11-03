package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ------------------------------------------------------------------ fakes

type memStore struct {
	mu       sync.Mutex
	nextID   int
	students map[int]Student
	users    map[string]User
	pingErr  error
	listErr  error
	nilList  bool // return a nil slice for an empty table, like a careless real store might
	reads    int  // number of ListStudents calls, to prove the cache is used
}

func newMemStore() *memStore {
	return &memStore{nextID: 1, students: map[int]Student{}, users: map[string]User{}}
}

func (m *memStore) Ping(context.Context) error { return m.pingErr }

func (m *memStore) ListStudents(context.Context) ([]Student, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []Student
	if !m.nilList {
		out = []Student{}
	}
	for _, s := range m.students {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memStore) emailTaken(email string, exceptID int) bool {
	for _, s := range m.students {
		if s.Email == email && s.ID != exceptID {
			return true
		}
	}
	return false
}

func (m *memStore) CreateStudent(_ context.Context, s Student) (Student, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.emailTaken(s.Email, 0) {
		return Student{}, ErrConflict
	}
	s.ID = m.nextID
	m.nextID++
	m.students[s.ID] = s
	return s, nil
}

func (m *memStore) UpdateStudent(_ context.Context, id int, s Student) (Student, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.students[id]; !ok {
		return Student{}, ErrNotFound
	}
	if m.emailTaken(s.Email, id) {
		return Student{}, ErrConflict
	}
	s.ID = id
	m.students[id] = s
	return s, nil
}

func (m *memStore) DeleteStudent(_ context.Context, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.students[id]; !ok {
		return ErrNotFound
	}
	delete(m.students, id)
	return nil
}

func (m *memStore) CreateUser(_ context.Context, email, hash string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[email]; ok {
		return User{}, ErrConflict
	}
	u := User{ID: len(m.users) + 1, Email: email, PasswordHash: hash}
	m.users[email] = u
	return u, nil
}

func (m *memStore) UserByEmail(_ context.Context, email string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

type memCache struct {
	mu      sync.Mutex
	data    map[string][]byte
	broken  bool // simulate Redis being down
	sets    int
	deletes int
}

func newMemCache() *memCache { return &memCache{data: map[string][]byte{}} }

func (c *memCache) Get(_ context.Context, k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken {
		return nil, false
	}
	v, ok := c.data[k]
	return v, ok
}

func (c *memCache) Set(_ context.Context, k string, v []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken {
		return errors.New("redis down")
	}
	c.sets++
	c.data[k] = v
	return nil
}

func (c *memCache) Delete(_ context.Context, k string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken {
		return errors.New("redis down")
	}
	c.deletes++
	delete(c.data, k)
	return nil
}

// ---------------------------------------------------------------- harness

const testSecret = "0123456789abcdef0123456789abcdef-test"

type harness struct {
	t       *testing.T
	srv     *Server
	store   *memStore
	cache   *memCache
	handler http.Handler
	emailed chan Student
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg, err := LoadConfig(mapEnv(map[string]string{
		"DATABASE_URL": "postgres://unused",
		"JWT_SECRET":   testSecret,
	}))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, store: newMemStore(), cache: newMemCache(), emailed: make(chan Student, 16)}
	h.srv, err = NewServer(cfg, h.store, h.cache, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	h.srv.notify = func(s Student) { h.emailed <- s }
	h.handler = h.srv.Handler()
	return h
}

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// do sends a request through the full handler stack and returns the recorder.
func (h *harness) do(method, path, token string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rdr = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			h.t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

// login registers a user (if needed) and returns a valid token.
func (h *harness) login() string {
	h.t.Helper()
	creds := Credentials{Email: "admin@campus.edu", Password: "correct horse"}
	if rec := h.do("POST", "/api/register", "", creds); rec.Code != http.StatusCreated && rec.Code != http.StatusConflict {
		h.t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	rec := h.do("POST", "/api/login", "", creds)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	return decode[struct{ Token string }](h.t, rec).Token
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not the expected JSON: %v\nbody: %s", err, rec.Body)
	}
	return v
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[map[string]string](t, rec)["error"]
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body)
	}
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
