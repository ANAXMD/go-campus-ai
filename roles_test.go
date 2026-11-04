package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestRegistrationAssignsRolesFromAdminEmails(t *testing.T) {
	h := newHarnessWith(t, map[string]string{"ADMIN_EMAILS": " Boss@Campus.edu , second@campus.edu ,"})

	for email, want := range map[string]string{
		"boss@campus.edu":   "admin",  // the configured address is case-insensitive and trimmed
		" BOSS2@campus.edu": "viewer", // near miss
		"second@campus.edu": "admin",
		"random@campus.edu": "viewer",
	} {
		rec := h.do("POST", "/api/register", "", Credentials{Email: email, Password: "longenough"})
		wantStatus(t, rec, http.StatusCreated)
		if got := decode[map[string]any](t, rec)["role"]; got != want {
			t.Errorf("%s registered as %v, want %s", email, got, want)
		}
	}
}

func TestLoginReportsTheRoleInTheResponseAndTheToken(t *testing.T) {
	h := newHarness(t)
	for email, want := range map[string]Role{adminEmail: RoleAdmin, viewerEmail: RoleViewer} {
		token := h.loginAs(email)

		claims := &tokenClaims{}
		if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return h.srv.cfg.JWTSecret, nil }); err != nil {
			t.Fatal(err)
		}
		if claims.Role != want {
			t.Errorf("%s: token role = %q, want %q", email, claims.Role, want)
		}
		rec := h.do("POST", "/api/login", "", Credentials{Email: email, Password: "correct horse"})
		if got := decode[map[string]string](t, rec)["role"]; got != string(want) {
			t.Errorf("%s: login response role = %q, want %q", email, got, want)
		}
	}
}

func TestViewersCanReadButNotWrite(t *testing.T) {
	h := newHarness(t)
	admin, viewer := h.login(), h.loginViewer()
	created := decode[Student](t, h.do("POST", "/api/students", admin, newStudent("Ada", "ada@campus.edu", "Math")))

	// Reading is allowed.
	if page, _ := getPage(t, h, viewer, ""); page.Total != 1 {
		t.Fatalf("viewer sees %+v", page)
	}
	wantStatus(t, h.do("GET", "/api/students/stats", viewer, nil), http.StatusOK)

	// Every write is forbidden, with 403 (signed in but not allowed), not 401.
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/students", newStudent("Alan", "alan@campus.edu", "Math")},
		{"PUT", "/api/students/" + itoa(created.ID), newStudent("Ada King", "ada@campus.edu", "Math")},
		{"DELETE", "/api/students/" + itoa(created.ID), nil},
	} {
		rec := h.do(tc.method, tc.path, viewer, tc.body)
		wantStatus(t, rec, http.StatusForbidden)
		if !strings.Contains(errorOf(t, rec), "admin") {
			t.Errorf("%s %s: error %q should say what is required", tc.method, tc.path, errorOf(t, rec))
		}
	}

	// And none of them changed anything.
	if page, _ := getPage(t, h, admin, ""); page.Total != 1 || page.Items[0].Name != "Ada" {
		t.Fatalf("a forbidden write changed the data: %+v", page)
	}
}

func TestAForbiddenWriteDoesNotInvalidateTheCache(t *testing.T) {
	h := newHarness(t)
	admin, viewer := h.login(), h.loginViewer()
	wantStatus(t, h.do("POST", "/api/students", admin, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
	listStudents(t, h, viewer)

	before := h.cache.incrs
	wantStatus(t, h.do("DELETE", "/api/students/1", viewer, nil), http.StatusForbidden)
	if h.cache.incrs != before {
		t.Fatal("a rejected request must not touch the cache")
	}
}

func TestAdminsCanWrite(t *testing.T) {
	h := newHarness(t)
	admin := h.login()
	created := decode[Student](t, h.do("POST", "/api/students", admin, newStudent("Ada", "ada@campus.edu", "Math")))
	wantStatus(t, h.do("PUT", "/api/students/"+itoa(created.ID), admin, newStudent("Ada King", "ada@campus.edu", "Math")), http.StatusOK)
	wantStatus(t, h.do("DELETE", "/api/students/"+itoa(created.ID), admin, nil), http.StatusNoContent)
}

func TestUnauthenticatedWritesAre401NotForbidden(t *testing.T) {
	h := newHarness(t)
	wantStatus(t, h.do("POST", "/api/students", "", newStudent("Ada", "ada@campus.edu", "Math")), http.StatusUnauthorized)
}

// The role comes from the token, so a stolen or stale token cannot be upgraded
// by editing it: the signature covers the role claim.
func TestAViewerCannotEditTheirTokenIntoAnAdmin(t *testing.T) {
	h := newHarness(t)
	viewer := h.loginViewer()
	parts := strings.Split(viewer, ".")

	forged := parts[0] + "." + b64(`{"email":"viewer@campus.edu","role":"admin","sub":"2","exp":9999999999}`) + "." + parts[2]
	wantStatus(t, h.do("POST", "/api/students", forged, newStudent("Mallory", "m@campus.edu", "Math")), http.StatusUnauthorized)
}

func TestInviteOnlyRegistration(t *testing.T) {
	h := newHarnessWith(t, map[string]string{"ALLOW_REGISTRATION": "false"})

	rec := h.do("POST", "/api/register", "", Credentials{Email: "stranger@campus.edu", Password: "longenough"})
	wantStatus(t, rec, http.StatusForbidden)
	if errorOf(t, rec) == "" {
		t.Error("the refusal should explain itself")
	}
	if _, ok := h.store.users["stranger@campus.edu"]; ok {
		t.Fatal("a refused registration still created an account")
	}

	// Only the configured administrators can create an account.
	wantStatus(t, h.do("POST", "/api/register", "", Credentials{Email: adminEmail, Password: "longenough"}), http.StatusCreated)
}

func TestInviteOnlyDoesNotAffectLogin(t *testing.T) {
	h := newHarnessWith(t, map[string]string{"ALLOW_REGISTRATION": "false"})
	wantStatus(t, h.do("POST", "/api/register", "", Credentials{Email: adminEmail, Password: "correct horse"}), http.StatusCreated)
	wantStatus(t, h.do("POST", "/api/login", "", Credentials{Email: adminEmail, Password: "correct horse"}), http.StatusOK)
}

func TestARoleChangeTakesEffectAtTheNextToken(t *testing.T) {
	h := newHarness(t)
	oldToken := h.loginViewer()
	wantStatus(t, h.do("POST", "/api/students", oldToken, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusForbidden)

	// An operator adds the address to ADMIN_EMAILS and restarts the API.
	if err := h.store.SyncAdmins(t.Context(), []string{viewerEmail}); err != nil {
		t.Fatal(err)
	}

	// The token already in the user's hands still says "viewer"...
	wantStatus(t, h.do("POST", "/api/students", oldToken, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusForbidden)
	// ...until they sign in again.
	rec := h.do("POST", "/api/login", "", Credentials{Email: viewerEmail, Password: "correct horse"})
	fresh := decode[struct{ Token string }](t, rec).Token
	wantStatus(t, h.do("POST", "/api/students", fresh, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
}
