package main

import (
	"context"
	"errors"
	"testing"
)

// runStoreContract is the specification of Store. It runs against the
// in-memory fake on every `go test`, and against the real Postgres
// implementation whenever TEST_DATABASE_URL is set (always, in CI).
//
// That is the point of having one suite: the handler tests use the fake, so
// the fake has to behave exactly like Postgres or those tests prove nothing.
// If the two ever disagree, this is where it shows up.
func runStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	ctx := context.Background()

	add := func(t *testing.T, s Store, name, email, major string) Student {
		t.Helper()
		got, err := s.CreateStudent(ctx, Student{Name: name, Email: email, Major: major})
		if err != nil {
			t.Fatalf("CreateStudent(%s): %v", email, err)
		}
		return got
	}
	list := func(t *testing.T, s Store, q StudentQuery) StudentPage {
		t.Helper()
		if q.Limit == 0 {
			q.Limit = 100
		}
		page, err := s.ListStudents(ctx, q)
		if err != nil {
			t.Fatalf("ListStudents(%+v): %v", q, err)
		}
		return page
	}
	names := func(page StudentPage) []string {
		out := []string{}
		for _, s := range page.Items {
			out = append(out, s.Name)
		}
		return out
	}
	equal := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	t.Run("ping", func(t *testing.T) {
		if err := newStore(t).Ping(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("an empty directory lists as an empty, non-nil page", func(t *testing.T) {
		page := list(t, newStore(t), StudentQuery{Limit: 10})
		if page.Items == nil || len(page.Items) != 0 || page.Total != 0 || page.Limit != 10 || page.Offset != 0 {
			t.Fatalf("page = %+v (nil items: %v)", page, page.Items == nil)
		}
	})

	t.Run("students get increasing ids and list in id order", func(t *testing.T) {
		s := newStore(t)
		a := add(t, s, "A", "a@x.io", "Art")
		b := add(t, s, "B", "b@x.io", "Art")
		c := add(t, s, "C", "c@x.io", "Art")
		if !(a.ID < b.ID && b.ID < c.ID) {
			t.Fatalf("ids %d, %d, %d are not increasing", a.ID, b.ID, c.ID)
		}
		if got := names(list(t, s, StudentQuery{})); !equal(got, []string{"A", "B", "C"}) {
			t.Fatalf("order = %v", got)
		}
	})

	t.Run("a duplicate email is a conflict", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "A", "a@x.io", "Art")
		if _, err := s.CreateStudent(ctx, Student{Name: "Other", Email: "a@x.io", Major: "Art"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
		if got := list(t, s, StudentQuery{}).Total; got != 1 {
			t.Fatalf("total = %d after a rejected insert", got)
		}
	})

	t.Run("pagination slices the ordered list and reports the full total", func(t *testing.T) {
		s := newStore(t)
		for _, n := range []string{"A", "B", "C", "D", "E", "F", "G"} {
			add(t, s, n, n+"@x.io", "Art")
		}
		for _, tc := range []struct {
			limit, offset int
			want          []string
		}{
			{3, 0, []string{"A", "B", "C"}},
			{3, 3, []string{"D", "E", "F"}},
			{3, 6, []string{"G"}},
			{3, 7, []string{}},
			{3, 100, []string{}},
			{1, 2, []string{"C"}},
			{100, 0, []string{"A", "B", "C", "D", "E", "F", "G"}},
		} {
			page := list(t, s, StudentQuery{Limit: tc.limit, Offset: tc.offset})
			if !equal(names(page), tc.want) || page.Total != 7 || page.Limit != tc.limit || page.Offset != tc.offset {
				t.Errorf("limit %d offset %d: %v, total %d, limit %d, offset %d; want %v, total 7",
					tc.limit, tc.offset, names(page), page.Total, page.Limit, page.Offset, tc.want)
			}
		}
	})

	t.Run("search matches name, email and major in any case", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "Ada Lovelace", "ada@campus.edu", "Mathematics")
		add(t, s, "Alan Turing", "alan@campus.edu", "Computer Science")
		add(t, s, "Grace Hopper", "grace@navy.mil", "Computer Science")
		for _, tc := range []struct {
			search string
			want   []string
		}{
			{"lovelace", []string{"Ada Lovelace"}},
			{"NAVY.MIL", []string{"Grace Hopper"}},
			{"computer", []string{"Alan Turing", "Grace Hopper"}},
			{"Science", []string{"Alan Turing", "Grace Hopper"}},
			{"ada", []string{"Ada Lovelace"}},
			{"nobody", []string{}},
			{"", []string{"Ada Lovelace", "Alan Turing", "Grace Hopper"}},
		} {
			page := list(t, s, StudentQuery{Search: tc.search})
			if !equal(names(page), tc.want) || page.Total != len(tc.want) {
				t.Errorf("search %q: %v (total %d), want %v", tc.search, names(page), page.Total, tc.want)
			}
		}
	})

	t.Run("search treats % _ and backslash as ordinary characters", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "Pct 100%", "p1@x.io", "Misc")
		add(t, s, "Under_score", "p2@x.io", "Misc")
		add(t, s, "Back\\slash", "p3@x.io", "Misc")
		add(t, s, "Plain", "p4@x.io", "Misc")
		for _, tc := range []struct{ search, want string }{
			{"%", "Pct 100%"},
			{"100%", "Pct 100%"},
			{"_", "Under_score"},
			{"r_s", "Under_score"},
			{`\`, "Back\\slash"},
		} {
			page := list(t, s, StudentQuery{Search: tc.search})
			if !equal(names(page), []string{tc.want}) || page.Total != 1 {
				t.Errorf("search %q matched %v (total %d), want only %q; the character is being treated as a wildcard",
					tc.search, names(page), page.Total, tc.want)
			}
		}
	})

	t.Run("search and pagination combine, with the total counting every match", func(t *testing.T) {
		s := newStore(t)
		// "al" is in exactly four of these (name and email alike); "Carl" is
		// c-a-r-l, which does not contain it, and neither does the major "Art".
		for _, n := range []string{"Alice", "Bob", "Alina", "Carl", "Alan", "Alex"} {
			add(t, s, n, n+"@x.io", "Art")
		}
		for _, tc := range []struct {
			limit, offset int
			want          []string
		}{
			{2, 0, []string{"Alice", "Alina"}},
			{2, 1, []string{"Alina", "Alan"}},
			{2, 3, []string{"Alex"}},
			{2, 4, []string{}},
		} {
			page := list(t, s, StudentQuery{Search: "al", Limit: tc.limit, Offset: tc.offset})
			if !equal(names(page), tc.want) || page.Total != 4 {
				t.Errorf("limit %d offset %d: %v, total %d; want %v, total 4", tc.limit, tc.offset, names(page), page.Total, tc.want)
			}
		}
	})

	t.Run("update changes the row, and reports missing rows and email clashes", func(t *testing.T) {
		s := newStore(t)
		a := add(t, s, "A", "a@x.io", "Art")
		b := add(t, s, "B", "b@x.io", "Art")

		got, err := s.UpdateStudent(ctx, a.ID, Student{Name: "A2", Email: "a@x.io", Major: "Music"}) // same email as before is fine
		if err != nil || got.ID != a.ID || got.Name != "A2" || got.Major != "Music" {
			t.Fatalf("update = %+v, %v", got, err)
		}
		if _, err := s.UpdateStudent(ctx, b.ID, Student{Name: "B", Email: "a@x.io", Major: "Art"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("clashing email: err = %v, want ErrConflict", err)
		}
		if _, err := s.UpdateStudent(ctx, 987654, Student{Name: "Ghost", Email: "g@x.io", Major: "Art"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing row: err = %v, want ErrNotFound", err)
		}
		if got := names(list(t, s, StudentQuery{})); !equal(got, []string{"A2", "B"}) {
			t.Fatalf("after updates: %v", got)
		}
	})

	t.Run("delete removes the row and reports a missing one", func(t *testing.T) {
		s := newStore(t)
		a := add(t, s, "A", "a@x.io", "Art")
		add(t, s, "B", "b@x.io", "Art")
		if err := s.DeleteStudent(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteStudent(ctx, a.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("second delete: err = %v, want ErrNotFound", err)
		}
		if got := names(list(t, s, StudentQuery{})); !equal(got, []string{"B"}) {
			t.Fatalf("after delete: %v", got)
		}
		// The freed email can be used again.
		add(t, s, "A again", "a@x.io", "Art")
	})

	t.Run("stats on an empty directory are all zero", func(t *testing.T) {
		got, err := newStore(t).StudentStats(ctx)
		if err != nil || got != (StudentStats{}) {
			t.Fatalf("stats = %+v, %v", got, err)
		}
	})

	t.Run("stats count students and majors and name the largest major", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "A", "a@x.io", "Mathematics")
		add(t, s, "B", "b@x.io", "Mathematics")
		add(t, s, "C", "c@x.io", "Art")
		got, err := s.StudentStats(ctx)
		if want := (StudentStats{Total: 3, Majors: 2, TopMajor: "Mathematics", TopCount: 2}); err != nil || got != want {
			t.Fatalf("stats = %+v, %v; want %+v", got, err, want)
		}
	})

	t.Run("a tie for largest major goes to the one that sorts first by bytes", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "A", "a@x.io", "apple")
		add(t, s, "B", "b@x.io", "Zoology")
		got, err := s.StudentStats(ctx)
		// By bytes "Zoology" sorts before "apple" (upper case before lower). A
		// database using a locale collation would pick "apple", which is why the
		// real query pins COLLATE "C".
		if err != nil || got.TopMajor != "Zoology" || got.TopCount != 1 || got.Majors != 2 {
			t.Fatalf("stats = %+v, %v", got, err)
		}
	})

	t.Run("users: create, look up, reject duplicates", func(t *testing.T) {
		s := newStore(t)
		admin, err := s.CreateUser(ctx, "boss@x.io", "hash-1", RoleAdmin)
		if err != nil || admin.ID == 0 || admin.Role != RoleAdmin {
			t.Fatalf("CreateUser = %+v, %v", admin, err)
		}
		if _, err := s.CreateUser(ctx, "viewer@x.io", "hash-2", RoleViewer); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateUser(ctx, "boss@x.io", "other", RoleViewer); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate: err = %v, want ErrConflict", err)
		}

		got, err := s.UserByEmail(ctx, "boss@x.io")
		if err != nil || got != admin || got.PasswordHash != "hash-1" {
			t.Fatalf("UserByEmail = %+v, %v; want %+v", got, err, admin)
		}
		if _, err := s.UserByEmail(ctx, "ghost@x.io"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown user: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("SyncAdmins makes exactly the listed accounts admins", func(t *testing.T) {
		s := newStore(t)
		for email, role := range map[string]Role{"a@x.io": RoleViewer, "b@x.io": RoleAdmin, "c@x.io": RoleViewer} {
			if _, err := s.CreateUser(ctx, email, "h", role); err != nil {
				t.Fatal(err)
			}
		}
		roles := func() string {
			out := ""
			for _, email := range []string{"a@x.io", "b@x.io", "c@x.io"} {
				u, err := s.UserByEmail(ctx, email)
				if err != nil {
					t.Fatal(err)
				}
				out += string(u.Role)[:1]
			}
			return out // e.g. "avv": a is an admin, b and c are viewers
		}

		// Promotes the listed account, demotes the previous admin, ignores an
		// address that has no account.
		if err := s.SyncAdmins(ctx, []string{"a@x.io", "ghost@x.io"}); err != nil {
			t.Fatal(err)
		}
		if got := roles(); got != "avv" {
			t.Fatalf("roles = %s, want avv", got)
		}
		// Syncing again changes nothing.
		if err := s.SyncAdmins(ctx, []string{"a@x.io"}); err != nil || roles() != "avv" {
			t.Fatalf("not idempotent: %s, %v", roles(), err)
		}
		// Several admins.
		if err := s.SyncAdmins(ctx, []string{"b@x.io", "c@x.io"}); err != nil || roles() != "vaa" {
			t.Fatalf("roles = %s, %v; want vaa", roles(), err)
		}
		// An empty list demotes everyone.
		if err := s.SyncAdmins(ctx, nil); err != nil || roles() != "vvv" {
			t.Fatalf("roles = %s, %v; want vvv", roles(), err)
		}
	})
}

func TestMemStoreContract(t *testing.T) {
	runStoreContract(t, func(*testing.T) Store { return newMemStore() })
}
