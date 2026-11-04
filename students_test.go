package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newStudent(name, email, major string) Student {
	return Student{Name: name, Email: email, Major: major}
}

// getPage fetches /api/students with the given query string and returns the
// decoded page and the X-Cache header.
func getPage(t *testing.T, h *harness, token, query string) (StudentPage, string) {
	t.Helper()
	path := "/api/students"
	if query != "" {
		path += "?" + query
	}
	rec := h.do("GET", path, token, nil)
	wantStatus(t, rec, http.StatusOK)
	return decode[StudentPage](t, rec), rec.Header().Get("X-Cache")
}

func listStudents(t *testing.T, h *harness, token string) ([]Student, string) {
	t.Helper()
	page, source := getPage(t, h, token, "")
	return page.Items, source
}

// seed creates n students named "Student 01", "Student 02", ... and returns them.
func seed(t *testing.T, h *harness, token string, n int) []Student {
	t.Helper()
	var out []Student
	for i := 1; i <= n; i++ {
		num := strconv.Itoa(i)
		if i < 10 {
			num = "0" + num
		}
		rec := h.do("POST", "/api/students", token, newStudent("Student "+num, "s"+num+"@campus.edu", "Major "+strconv.Itoa(i%3)))
		wantStatus(t, rec, http.StatusCreated)
		out = append(out, decode[Student](t, rec))
	}
	return out
}

func TestStudentsRequireAuth(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/students"},
		{"GET", "/api/students/stats"},
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
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("body = %s, want an empty items array and not null", rec.Body)
	}
}

func TestListEncodesANilSliceFromTheStoreAsAnEmptyArray(t *testing.T) {
	h := newHarness(t)
	h.store.nilList = true
	rec := h.do("GET", "/api/students", h.login(), nil)
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("body = %s, want an empty items array and not null", rec.Body)
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

// ------------------------------------------------------------- pagination

func TestPaginationWalksEveryStudentExactlyOnce(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	all := seed(t, h, token, 7)

	var seen []int
	for offset := 0; offset < 9; offset += 3 {
		page, _ := getPage(t, h, token, "limit=3&offset="+itoa(offset))
		if page.Total != 7 || page.Limit != 3 || page.Offset != offset {
			t.Fatalf("offset %d: page = %+v, want total 7, limit 3, offset %d", offset, page, offset)
		}
		for _, s := range page.Items {
			seen = append(seen, s.ID)
		}
	}
	if len(seen) != len(all) {
		t.Fatalf("saw %d students across the pages, want %d (ids %v)", len(seen), len(all), seen)
	}
	for i, s := range all {
		if seen[i] != s.ID {
			t.Fatalf("pages are out of order or repeat rows: %v", seen)
		}
	}
}

func TestPaginationDefaultsAndLimits(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	seed(t, h, token, 30)

	page, _ := getPage(t, h, token, "")
	if page.Limit != defaultLimit || len(page.Items) != defaultLimit || page.Total != 30 {
		t.Fatalf("default page: limit %d, %d items, total %d", page.Limit, len(page.Items), page.Total)
	}
	if page, _ := getPage(t, h, token, "limit="+itoa(maxLimit)); page.Limit != maxLimit {
		t.Fatalf("limit at the maximum was not honoured: %+v", page.Limit)
	}
	if page, _ := getPage(t, h, token, "offset=100"); len(page.Items) != 0 || page.Total != 30 {
		t.Fatalf("past the end: %d items, total %d; want no items but the real total", len(page.Items), page.Total)
	}
}

func TestBadPagingParametersAreRejected(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	for _, query := range []string{
		"limit=0", "limit=-1", "limit=" + itoa(maxLimit+1), "limit=abc", "limit=2.5",
		"offset=-1", "offset=abc",
		"q=" + strings.Repeat("x", maxSearchLen+1),
	} {
		rec := h.do("GET", "/api/students?"+query, token, nil)
		if rec.Code != http.StatusBadRequest || errorOf(t, rec) == "" {
			t.Errorf("?%s -> %d %s, want 400 with a message", query, rec.Code, rec.Body)
		}
	}
}

// ----------------------------------------------------------------- search

func TestSearchMatchesNameEmailAndMajorIgnoringCase(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	for _, s := range []Student{
		newStudent("Ada Lovelace", "ada@campus.edu", "Mathematics"),
		newStudent("Alan Turing", "alan@campus.edu", "Computer Science"),
		newStudent("Grace Hopper", "grace@navy.mil", "Computer Science"),
	} {
		wantStatus(t, h.do("POST", "/api/students", token, s), http.StatusCreated)
	}

	for query, want := range map[string]int{
		"q=lovelace":      1, // name
		"q=NAVY.MIL":      1, // email, upper-case
		"q=computer":      2, // major
		"q=%20%20science": 2, // surrounding whitespace is ignored
		"q=a":             3,
		"q=nobody":        0,
		"q=":              3, // empty search means everyone
	} {
		page, _ := getPage(t, h, token, query)
		if page.Total != want || len(page.Items) != want {
			t.Errorf("?%s -> total %d, %d items; want %d", query, page.Total, len(page.Items), want)
		}
	}
}

func TestSearchAndPagingCombine(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	seed(t, h, token, 20) // majors cycle through "Major 0", "Major 1", "Major 2"

	page, _ := getPage(t, h, token, "q="+url.QueryEscape("major 1")+"&limit=3&offset=3")
	if page.Total != 7 || len(page.Items) != 3 {
		t.Fatalf("page = %+v, want 7 matches in total and 3 on this page", page)
	}
	for _, s := range page.Items {
		if s.Major != "Major 1" {
			t.Fatalf("%+v does not match the search", s)
		}
	}
}

// ---------------------------------------------------------------- stats

func TestStats(t *testing.T) {
	h := newHarness(t)
	token := h.login()

	rec := h.do("GET", "/api/students/stats", token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decode[StudentStats](t, rec); got != (StudentStats{}) {
		t.Fatalf("empty directory stats = %+v, want all zero", got)
	}

	for _, s := range []Student{
		newStudent("A", "a@campus.edu", "Mathematics"),
		newStudent("B", "b@campus.edu", "Mathematics"),
		newStudent("C", "c@campus.edu", "Art"),
	} {
		wantStatus(t, h.do("POST", "/api/students", token, s), http.StatusCreated)
	}
	got := decode[StudentStats](t, h.do("GET", "/api/students/stats", token, nil))
	if want := (StudentStats{Total: 3, Majors: 2, TopMajor: "Mathematics", TopCount: 2}); got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

func TestStatsAreCachedAndRefreshedByWrites(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("A", "a@campus.edu", "Art")), http.StatusCreated)

	stats := func() (StudentStats, string) {
		rec := h.do("GET", "/api/students/stats", token, nil)
		wantStatus(t, rec, http.StatusOK)
		return decode[StudentStats](t, rec), rec.Header().Get("X-Cache")
	}
	if _, source := stats(); source != "MISS" {
		t.Fatalf("first read X-Cache = %s", source)
	}
	if got, source := stats(); source != "HIT" || got.Total != 1 {
		t.Fatalf("second read: %+v X-Cache=%s", got, source)
	}
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("B", "b@campus.edu", "Art")), http.StatusCreated)
	if got, source := stats(); source != "MISS" || got.Total != 2 {
		t.Fatalf("after a write: %+v X-Cache=%s, want a fresh MISS with total 2", got, source)
	}
}

// ---------------------------------------------------------------- caching

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

// Every distinct page, and every distinct search, is cached under its own key.
// If the key left out the paging or the search, one page would be served for
// all of them.
func TestEachPageAndSearchHasItsOwnCacheEntry(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	seed(t, h, token, 6)

	first, _ := getPage(t, h, token, "limit=2&offset=0")
	second, source := getPage(t, h, token, "limit=2&offset=2")
	if source != "MISS" || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("page two was answered from page one's cache entry: %+v", second)
	}
	if _, source := getPage(t, h, token, "limit=2&offset=2"); source != "HIT" {
		t.Fatalf("repeat of page two X-Cache = %s, want HIT", source)
	}

	searched, source := getPage(t, h, token, "limit=2&offset=0&q=student+06")
	if source != "MISS" || searched.Total != 1 {
		t.Fatalf("a search was answered from the unfiltered entry: %+v X-Cache=%s", searched, source)
	}
	// ILIKE ignores case, so "STUDENT 06" is the same search and shares the entry.
	if _, source := getPage(t, h, token, "limit=2&offset=0&q=STUDENT+06"); source != "HIT" {
		t.Fatalf("same search in another case X-Cache = %s, want HIT", source)
	}
}

// The original version cached the list for ten minutes and never cleared it,
// so a student you had just added did not appear. These tests pin the fix, for
// every kind of write.
func TestEveryWriteInvalidatesEveryCachedPage(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	first := decode[Student](t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")))

	warm := func() {
		t.Helper()
		getPage(t, h, token, "limit=1")
		getPage(t, h, token, "q=ada")
		for _, q := range []string{"limit=1", "q=ada"} {
			if _, source := getPage(t, h, token, q); source != "HIT" {
				t.Fatalf("?%s did not warm up", q)
			}
		}
	}

	warm()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Alan", "alan@campus.edu", "Math")), http.StatusCreated)
	if page, source := getPage(t, h, token, "limit=1"); source != "MISS" || page.Total != 2 {
		t.Fatalf("after create: total %d, X-Cache=%s", page.Total, source)
	}
	if _, source := getPage(t, h, token, "q=ada"); source != "MISS" {
		t.Fatalf("after create the search page was still served from cache")
	}

	warm()
	wantStatus(t, h.do("PUT", "/api/students/"+itoa(first.ID), token, newStudent("Ada King", "ada@campus.edu", "Math")), http.StatusOK)
	if page, source := getPage(t, h, token, "q=ada"); source != "MISS" || page.Items[0].Name != "Ada King" {
		t.Fatalf("after update: %+v, X-Cache=%s", page, source)
	}

	warm()
	wantStatus(t, h.do("DELETE", "/api/students/"+itoa(first.ID), token, nil), http.StatusNoContent)
	if page, source := getPage(t, h, token, "limit=1"); source != "MISS" || page.Total != 1 {
		t.Fatalf("after delete: total %d, X-Cache=%s", page.Total, source)
	}
}

// This is the race that delete-on-write cannot survive, and the reason the
// cache is versioned. A reader fetches rows from the database; before it
// stores them, a writer commits and invalidates. With delete-on-write the
// reader then stores its now-stale rows after the delete and they are served
// for up to the TTL. Versioned keys send the reader's entry to a generation
// that nobody reads.
func TestSlowReaderCannotLeaveAStaleEntryBehind(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)

	h.store.afterList = func() {
		h.store.afterList = nil
		wantStatus(t, h.do("POST", "/api/students", token, newStudent("Alan", "alan@campus.edu", "Math")), http.StatusCreated)
	}
	if page, _ := getPage(t, h, token, ""); page.Total != 1 {
		t.Fatalf("the slow reader should have seen the old data, got total %d", page.Total)
	}

	page, source := getPage(t, h, token, "")
	if page.Total != 2 || source != "MISS" {
		t.Fatalf("after the writer finished: total %d, X-Cache=%s; a stale entry was cached", page.Total, source)
	}
}

func TestFailedWritesLeaveTheCacheAlone(t *testing.T) {
	h := newHarness(t)
	token := h.login()
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
	listStudents(t, h, token)

	before := h.cache.incrs
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Dup", "ada@campus.edu", "Math")), http.StatusConflict)
	wantStatus(t, h.do("POST", "/api/students", token, newStudent("", "x@campus.edu", "Math")), http.StatusBadRequest)
	wantStatus(t, h.do("DELETE", "/api/students/999", token, nil), http.StatusNotFound)
	if h.cache.incrs != before {
		t.Fatal("a rejected write should not invalidate a still-correct cache")
	}
	if _, source := listStudents(t, h, token); source != "HIT" {
		t.Fatalf("X-Cache = %s, want the warm entry to survive the failed writes", source)
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
	wantStatus(t, h.do("GET", "/api/students/stats", token, nil), http.StatusOK)
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

// ---------------------------------------------------------------- other

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
