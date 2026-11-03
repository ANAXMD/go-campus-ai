package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newStudent(name, email, major string) Student {
	return Student{Name: name, Email: email, Major: major}
}

func listStudents(t *testing.T, h *harness, token string) ([]Student, string) {
	t.Helper()
	rec := h.do("GET", "/api/students", token, nil)
	wantStatus(t, rec, http.StatusOK)
	return decode[[]Student](t, rec), rec.Header().Get("X-Cache")
}

func TestStudentsRequireAuth(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/students"},
		{"POST", "/api/students"},
		{"PUT", "/api/students/1"},
		{"DELETE", "/api/students/1"},
	} {
		wantStatus(t, h.do(tc.method, tc.path, "", nil), http.StatusUnauthorized)
	}
}

func TestListEmptyIsAnEmptyArrayNotNull(t *testing.T) {
	h := newHarness(t)
	rec := h.do("GET", "/api/students", h.login(), nil)
	wantStatus(t, rec, http.StatusOK)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestListEncodesANilSliceFromTheStoreAsAnEmptyArray(t *testing.T) {
	h := newHarness(t)
	h.store.nilList = true
	rec := h.do("GET", "/api/students", h.login(), nil)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestCreateListUpdateDelete(t *testing.T) {
	h := newHarness(t)
	token := h.login()

	rec := h.do("POST", "/api/students", token, newStudent("  Ada Lovelace ", "ada@campus.edu", "Mathematics"))
	wantStatus(t, rec, http.StatusCreated)
	created := decode[Student](t, rec)
	if created.ID == 0 || created.Name != "Ada Lovelace" {
		t.Fatalf("created = %+v (input should be trimmed and given an id)", created)
	}

	students, _ := listStudents(t, h, token)
	if len(students) != 1 || students[0] != created {
		t.Fatalf("list = %+v", students)
	}

	rec = h.do("PUT", "/api/students/"+itoa(created.ID), token, newStudent("Ada King", "ada@campus.edu", "Computer Science"))
	wantStatus(t, rec, http.StatusOK)
	if got := decode[Student](t, rec); got.ID != created.ID || got.Major != "Computer Science" {
		t.Fatalf("updated = %+v", got)
	}

	wantStatus(t, h.do("DELETE", "/api/students/"+itoa(created.ID), token, nil), http.StatusNoContent)
	if students, _ := listStudents(t, h, token); len(students) != 0 {
		t.Fatalf("list after delete = %+v", students)
	}
}

func TestValidation(t *testing.T) {
	long := strings.Repeat("x", 101)
	tests := []struct {
		name string
		in   Student
	}{
		{"blank name", newStudent("   ", "a@campus.edu", "Math")},
		{"name too long", newStudent(long, "a@campus.edu", "Math")},
		{"bad email", newStudent("Ada", "not-an-email", "Math")},
		{"blank major", newStudent("Ada", "a@campus.edu", "")},
		{"major too long", newStudent("Ada", "a@campus.edu", strings.Repeat("m", 51))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			rec := h.do("POST", "/api/students", h.login(), tt.in)
			wantStatus(t, rec, http.StatusBadRequest)
			if errorOf(t, rec) == "" {
				t.Error("missing error message")
			}
		})
	}
}

func TestConflictsAndMissingRows(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
	other := decode[Student](t, h.do("POST", "/api/students", token, newStudent("Alan", "alan@campus.edu", "Math")))

	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Imposter", "ada@campus.edu", "Math")), http.StatusConflict)
	wantStatus(t, h.do("PUT", "/api/students/"+itoa(other.ID), token, newStudent("Alan", "ada@campus.edu", "Math")), http.StatusConflict)
	wantStatus(t, h.do("PUT", "/api/students/999", token, newStudent("Ghost", "ghost@campus.edu", "Math")), http.StatusNotFound)
	wantStatus(t, h.do("DELETE", "/api/students/999", token, nil), http.StatusNotFound)
	for _, bad := range []string{"abc", "0", "-3", "1.5"} {
		wantStatus(t, h.do("DELETE", "/api/students/"+bad, token, nil), http.StatusBadRequest)
	}
}

func TestListIsCachedAfterFirstRead(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)

	if _, source := listStudents(t, h, token); source != "MISS" {
		t.Fatalf("first read X-Cache = %q, want MISS", source)
	}
	readsAfterFirst := h.store.reads
	for i := 0; i < 3; i++ {
		if _, source := listStudents(t, h, token); source != "HIT" {
			t.Fatalf("repeat read X-Cache = %q, want HIT", source)
		}
	}
	if h.store.reads != readsAfterFirst {
		t.Fatalf("database was read %d extra times despite a warm cache", h.store.reads-readsAfterFirst)
	}
}

// The original version cached the list for ten minutes and never cleared it,
// so a student you had just added did not appear. These tests pin the fix.
func TestEveryWriteInvalidatesTheCachedList(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	first := decode[Student](t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")))

	warm := func() {
		t.Helper()
		listStudents(t, h, token)
		if _, source := listStudents(t, h, token); source != "HIT" {
			t.Fatal("cache did not warm up")
		}
	}

	warm()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Alan", "alan@campus.edu", "Math")), http.StatusCreated)
	if students, source := listStudents(t, h, token); source != "MISS" || len(students) != 2 {
		t.Fatalf("after create: %d students, X-Cache=%s", len(students), source)
	}

	warm()
	wantStatus(t, h.do("PUT", "/api/students/"+itoa(first.ID), token, newStudent("Ada King", "ada@campus.edu", "Math")), http.StatusOK)
	students, source := listStudents(t, h, token)
	if source != "MISS" || students[0].Name != "Ada King" {
		t.Fatalf("after update: %+v, X-Cache=%s", students, source)
	}

	warm()
	wantStatus(t, h.do("DELETE", "/api/students/"+itoa(first.ID), token, nil), http.StatusNoContent)
	if students, source := listStudents(t, h, token); source != "MISS" || len(students) != 1 {
		t.Fatalf("after delete: %d students, X-Cache=%s", len(students), source)
	}
}

func TestFailedWritesLeaveTheCacheAlone(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
	listStudents(t, h, token)

	before := h.cache.deletes
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Dup", "ada@campus.edu", "Math")), http.StatusConflict)
	wantStatus(t, h.do("DELETE", "/api/students/999", token, nil), http.StatusNotFound)
	if h.cache.deletes != before {
		t.Fatal("a rejected write should not evict a still-correct cache entry")
	}
}

func TestAPIKeepsWorkingWhenRedisIsDown(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	h.cache.broken = true

	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
	students, source := listStudents(t, h, token)
	if len(students) != 1 || source != "MISS" {
		t.Fatalf("students=%+v X-Cache=%s", students, source)
	}
}

func TestDatabaseErrorsAreHiddenFromClients(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	h.store.listErr = errors.New(`pq: password authentication failed for user "admin"`)

	rec := h.do("GET", "/api/students", token, nil)
	wantStatus(t, rec, http.StatusInternalServerError)
	if strings.Contains(rec.Body.String(), "admin") || strings.Contains(rec.Body.String(), "pq:") {
		t.Fatalf("internal detail leaked to the client: %s", rec.Body)
	}
}

func TestWelcomeEmailIsSentInTheBackground(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)

	h.srv.Wait()
	select {
	case got := <-h.emailed:
		if got.Email != "ada@campus.edu" || got.ID == 0 {
			t.Fatalf("notified about %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no welcome email was sent")
	}
}

func TestSlowEmailDoesNotDelayTheResponse(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	release := make(chan struct{})
	h.srv.notify = func(Student) { <-release }

	done := make(chan struct{})
	go func() {
		wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("request blocked on the email being sent")
	}
	close(release)
	h.srv.Wait()
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	rec := h.do("GET", "/api/health", "", nil)
	wantStatus(t, rec, http.StatusOK)
	if decode[map[string]string](t, rec)["status"] != "ok" {
		t.Fatalf("body = %s", rec.Body)
	}

	h.store.pingErr = errors.New("connection refused")
	wantStatus(t, h.do("GET", "/api/health", "", nil), http.StatusServiceUnavailable)
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	h := newHarness(t)
	for origin, want := range map[string]string{
		"http://localhost:3000": "http://localhost:3000",
		"https://evil.example":  "",
	} {
		req, _ := http.NewRequest("GET", "/api/health", nil)
		req.Header.Set("Origin", origin)
		rec := newRecorder()
		h.handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("origin %s: Allow-Origin = %q, want %q", origin, got, want)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
