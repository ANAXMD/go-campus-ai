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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ------------------------------------------------------------------ fakes

// memStore is an in-memory Store. It must behave exactly like PGStore;
// store_contract_test.go runs the same suite against both to prove it.
type memStore struct {
	mu       sync.Mutex
	nextID   int
	students map[int]Student
	users    map[string]User
	pingErr  error
	listErr  error
	nilList  bool // return a nil slice for an empty page, like a careless real store might
	reads    int  // number of ListStudents and StudentStats calls, to prove the cache is used

	// afterList runs after ListStudents has read its data but before it returns,
	// which lets a test slip a write in at the worst possible moment.
	afterList func()
}

func newMemStore() *memStore {
	return &memStore{nextID: 1, students: map[int]Student{}, users: map[string]User{}}
}

func (m *memStore) Ping(context.Context) error { return m.pingErr }

func (m *memStore) ListStudents(_ context.Context, q StudentQuery) (StudentPage, error) {
	m.mu.Lock()
	m.reads++
	if m.listErr != nil {
		m.mu.Unlock()
		return StudentPage{}, m.listErr
	}

	needle := strings.ToLower(q.Search)
	var matched []Student
	for _, s := range m.students {
		if needle == "" ||
			strings.Contains(strings.ToLower(s.Name), needle) ||
			strings.Contains(strings.ToLower(s.Email), needle) ||
			strings.Contains(strings.ToLower(s.Major), needle) {
			matched = append(matched, s)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

	page := StudentPage{Total: len(matched), Limit: q.Limit, Offset: q.Offset}
	if !m.nilList {
		page.Items = []Student{}
	}
	if q.Offset < len(matched) {
		end := min(q.Offset+q.Limit, len(matched))
		page.Items = append(page.Items, matched[q.Offset:end]...)
	}
	hook := m.afterList
	m.mu.Unlock()

	if hook != nil {
		hook()
	}
	return page, nil
}

func (m *memStore) StudentStats(context.Context) (StudentStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	counts := map[string]int{}
	for _, s := range m.students {
		counts[s.Major]++
	}
	st := StudentStats{Total: len(m.students), Majors: len(counts)}
	for major, n := range counts {
		// Most students wins; ties go to the major that sorts first by bytes.
		if n > st.TopCount || (n == st.TopCount && major < st.TopMajor) {
			st.TopMajor, st.TopCount = major, n
		}
	}
	return st, nil
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

func (m *memStore) CreateUser(_ context.Context, email, hash string, role Role) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[email]; ok {
		return User{}, ErrConflict
	}
	u := User{ID: len(m.users) + 1, Email: email, PasswordHash: hash, Role: role}
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

func (m *memStore) SyncAdmins(_ context.Context, emails []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	admin := map[string]bool{}
	for _, e := range emails {
		admin[e] = true
	}
	for email, u := range m.users {
		u.Role = RoleViewer
		if admin[email] {
			u.Role = RoleAdmin
		}
		m.users[email] = u
	}
	return nil
}

type memCache struct {
	mu     sync.Mutex
	data   map[string][]byte
	broken bool // simulate Redis being down
	sets   int
	incrs  int
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

func (c *memCache) Incr(_ context.Context, k string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken {
		return 0, errors.New("redis down")
	}
	c.incrs++
	n, _ := strconv.ParseInt(string(c.data[k]), 10, 64)
	n++
	c.data[k] = []byte(strconv.FormatInt(n, 10))
	return n, nil
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

const (
	adminEmail  = "admin@campus.edu"
	viewerEmail = "viewer@campus.edu"
)

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, nil)
}

// newHarnessWith builds a harness whose configuration is the test defaults
// plus extra environment variables.
func newHarnessWith(t *testing.T, extra map[string]string) *harness {
	t.Helper()
	env := map[string]string{
		"DATABASE_URL": "postgres://unused",
		"JWT_SECRET":   testSecret,
		"ADMIN_EMAILS": adminEmail,
	}
	for k, v := range extra {
		env[k] = v
	}
	cfg, err := LoadConfig(mapEnv(env))
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

// loginAs registers an account (if needed) and returns a valid token for it.
func (h *harness) loginAs(email string) string {
	h.t.Helper()
	creds := Credentials{Email: email, Password: "correct horse"}
	if rec := h.do("POST", "/api/register", "", creds); rec.Code != http.StatusCreated && rec.Code != http.StatusConflict {
		h.t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	rec := h.do("POST", "/api/login", "", creds)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	return decode[struct{ Token string }](h.t, rec).Token
}

// login returns an admin token; loginViewer returns a read-only one.
func (h *harness) login() string       { h.t.Helper(); return h.loginAs(adminEmail) }
func (h *harness) loginViewer() string { h.t.Helper(); return h.loginAs(viewerEmail) }

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
