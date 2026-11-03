package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost     = 12 // ~250 ms per hash; cost 14 made every register/login take over a second
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt silently ignores (or rejects) anything past 72 bytes
)

type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// tokenClaims is the JWT payload: the standard registered claims plus the
// email, so the UI can show who is signed in without another request.
type tokenClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

type ctxKey int

const userIDKey ctxKey = iota

// UserIDFrom returns the authenticated user's id that RequireJWT stored in
// the request context.
func UserIDFrom(ctx context.Context) (int, bool) {
	id, ok := ctx.Value(userIDKey).(int)
	return id, ok
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	if err := decodeJSON(w, r, &creds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	creds.Email = strings.ToLower(strings.TrimSpace(creds.Email))

	switch {
	case !validEmail(creds.Email) || len(creds.Email) > 100:
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	case len(creds.Password) < minPasswordLen:
		writeError(w, http.StatusBadRequest, "password must be at least "+strconv.Itoa(minPasswordLen)+" characters")
		return
	case len(creds.Password) > maxPasswordLen:
		writeError(w, http.StatusBadRequest, "password must be at most "+strconv.Itoa(maxPasswordLen)+" bytes")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(creds.Password), bcryptCost)
	if err != nil {
		s.internalError(w, "hash password", err)
		return
	}

	user, err := s.store.CreateUser(r.Context(), creds.Email, string(hash))
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusConflict, "an account with this email already exists")
		return
	}
	if err != nil {
		s.internalError(w, "create user", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": user.ID, "email": user.Email})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	if err := decodeJSON(w, r, &creds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	creds.Email = strings.ToLower(strings.TrimSpace(creds.Email))

	user, err := s.store.UserByEmail(r.Context(), creds.Email)
	switch {
	case errors.Is(err, ErrNotFound):
		// Burn the same CPU a real comparison would, so response time does not
		// reveal whether an email is registered.
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(creds.Password))
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	case err != nil:
		s.internalError(w, "look up user", err)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	now := s.now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims{
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.Itoa(user.ID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.TokenTTL)),
		},
	})
	signed, err := token.SignedString(s.cfg.JWTSecret)
	if err != nil {
		s.internalError(w, "sign token", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": signed, "email": user.Email})
}

// RequireJWT rejects any request without a valid, unexpired HS256 token.
func (s *Server) RequireJWT(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme, raw, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(raw) == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		claims := &tokenClaims{}
		token, err := jwt.ParseWithClaims(strings.TrimSpace(raw), claims,
			func(*jwt.Token) (any, error) { return s.cfg.JWTSecret, nil },
			// Pin the algorithm. Without this, the library trusts whatever
			// algorithm the token's own header claims.
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		)
		// The library only checks an expiry if one is present, so a token
		// signed without one would otherwise be valid forever.
		if err != nil || !token.Valid || claims.ExpiresAt == nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		userID, err := strconv.Atoi(claims.Subject)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
	}
}

// hashForTiming builds the throwaway hash used to equalise login timing.
func hashForTiming() ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcryptCost)
}
