package main

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestRegister(t *testing.T) {
	tests := []struct {
		name string
		body any
		want int
	}{
		{"ok", Credentials{"a@campus.edu", "longenough"}, http.StatusCreated},
		{"email is case-insensitive and trimmed", Credentials{"  A@Campus.EDU ", "longenough"}, http.StatusCreated},
		{"not an email", Credentials{"nope", "longenough"}, http.StatusBadRequest},
		{"display-name email rejected", Credentials{"Bob <a@campus.edu>", "longenough"}, http.StatusBadRequest},
		{"password too short", Credentials{"a@campus.edu", "short"}, http.StatusBadRequest},
		{"password past bcrypt limit", Credentials{"a@campus.edu", strings.Repeat("x", 73)}, http.StatusBadRequest},
		{"malformed json", "{not json", http.StatusBadRequest},
		{"trailing garbage", `{"email":"a@campus.edu","password":"longenough"} {}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			rec := h.do("POST", "/api/register", "", tt.body)
			wantStatus(t, rec, tt.want)
			if tt.want != http.StatusCreated && errorOf(t, rec) == "" {
				t.Error("error responses must carry an error message")
			}
		})
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	h := newHarness(t)
	creds := Credentials{"a@campus.edu", "longenough"}
	wantStatus(t, h.do("POST", "/api/register", "", creds), http.StatusCreated)
	wantStatus(t, h.do("POST", "/api/register", "", Credentials{"A@CAMPUS.EDU", "otherpass1"}), http.StatusConflict)
}

func TestRegisterNeverStoresPlaintext(t *testing.T) {
	h := newHarness(t)
	wantStatus(t, h.do("POST", "/api/register", "", Credentials{"a@campus.edu", "longenough"}), http.StatusCreated)
	if got := h.store.users["a@campus.edu"].PasswordHash; got == "" || got == "longenough" {
		t.Fatalf("stored hash = %q", got)
	}
}

func TestLogin(t *testing.T) {
	h := newHarness(t)
	wantStatus(t, h.do("POST", "/api/register", "", Credentials{"a@campus.edu", "longenough"}), http.StatusCreated)

	t.Run("success returns a token and the email", func(t *testing.T) {
		rec := h.do("POST", "/api/login", "", Credentials{"A@campus.edu", "longenough"})
		wantStatus(t, rec, http.StatusOK)
		out := decode[map[string]string](t, rec)
		if out["token"] == "" || out["email"] != "a@campus.edu" {
			t.Fatalf("response = %v", out)
		}
	})

	t.Run("wrong password and unknown user are indistinguishable", func(t *testing.T) {
		wrong := h.do("POST", "/api/login", "", Credentials{"a@campus.edu", "wrongpass1"})
		unknown := h.do("POST", "/api/login", "", Credentials{"ghost@campus.edu", "longenough"})
		wantStatus(t, wrong, http.StatusUnauthorized)
		wantStatus(t, unknown, http.StatusUnauthorized)
		if wrong.Body.String() != unknown.Body.String() {
			t.Errorf("bodies differ, which leaks which emails exist:\n%s\n%s", wrong.Body, unknown.Body)
		}
	})
}

func TestTokenCarriesExpectedClaims(t *testing.T) {
	h := newHarness(t)
	token := h.login()

	claims := &tokenClaims{}
	if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return h.srv.cfg.JWTSecret, nil }); err != nil {
		t.Fatal(err)
	}
	if claims.Email != "admin@campus.edu" || claims.Subject != "1" {
		t.Errorf("claims = %+v", claims)
	}
	if got := time.Until(claims.ExpiresAt.Time); got < 23*time.Hour || got > 25*time.Hour {
		t.Errorf("token expires in %v, want about 24h", got)
	}
}

func TestRequireJWT(t *testing.T) {
	h := newHarness(t)
	good := h.login()

	signWith := func(secret string, exp time.Time) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims{
			Email:            "x@campus.edu",
			RegisteredClaims: jwt.RegisteredClaims{Subject: strconv.Itoa(1), ExpiresAt: jwt.NewNumericDate(exp)},
		})
		s, err := tok.SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	// A correctly signed token that uses a different HMAC algorithm than the
	// one the server issues must still be refused (algorithm pinning).
	hs512 := func() string {
		signing := b64(`{"alg":"HS512","typ":"JWT"}`) + "." + b64(`{"sub":"1","exp":9999999999}`)
		mac := hmac.New(sha512.New, []byte(testSecret))
		mac.Write([]byte(signing))
		return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}()
	noExp, err := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "1"},
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	// {"alg":"none"} . {"sub":"1","exp":9999999999} . (no signature)
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIxIiwiZXhwIjo5OTk5OTk5OTk5fQ."

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"valid token", "Bearer " + good, http.StatusOK},
		{"scheme is case-insensitive", "bearer " + good, http.StatusOK},
		{"no header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic " + good, http.StatusUnauthorized},
		{"token without scheme", good, http.StatusUnauthorized},
		{"empty bearer", "Bearer ", http.StatusUnauthorized},
		{"garbage token", "Bearer abc.def.ghi", http.StatusUnauthorized},
		{"signed with another secret", "Bearer " + signWith("another-secret-another-secret-123456", time.Now().Add(time.Hour)), http.StatusUnauthorized},
		{"expired", "Bearer " + signWith(testSecret, time.Now().Add(-time.Hour)), http.StatusUnauthorized},
		{"alg none", "Bearer " + noneToken, http.StatusUnauthorized},
		{"correctly signed but HS512", "Bearer " + hs512, http.StatusUnauthorized},
		{"valid signature but no expiry", "Bearer " + noExp, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/students", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := newRecorder()
			h.handler.ServeHTTP(rec, req)
			wantStatus(t, rec, tt.want)
		})
	}
}

func TestRequireJWTPutsUserIDInContext(t *testing.T) {
	h := newHarness(t)
	token := h.login()

	var got int
	var ok bool
	protected := h.srv.RequireJWT(func(w http.ResponseWriter, r *http.Request) {
		got, ok = UserIDFrom(r.Context())
	})
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	protected(newRecorder(), req)
	if !ok || got != 1 {
		t.Fatalf("UserIDFrom = %d, %v", got, ok)
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
