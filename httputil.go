package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
)

const maxBodyBytes = 1 << 20 // 1 MiB is far more than any request here needs

// writeJSON encodes v as the response body. Encoding errors can only happen
// after the header is sent, so there is nothing useful left to do with them.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends {"error": "..."} so every failure has the same shape and
// the front end can show the message without guessing at the format.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads a single JSON object from the request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return fmt.Errorf("request body must be at most %d bytes", maxBodyBytes)
		}
		return errors.New("request body must be valid JSON")
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

// validEmail accepts "a@b.c" but not display-name forms like "Bob <a@b.c>".
func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// validateStudent trims the input in place and returns the first problem. The
// length limits mirror the column sizes in the migration.
func validateStudent(s *Student) error {
	s.Name = strings.TrimSpace(s.Name)
	s.Email = strings.TrimSpace(s.Email)
	s.Major = strings.TrimSpace(s.Major)

	switch {
	case s.Name == "" || len(s.Name) > 100:
		return errors.New("name is required and must be at most 100 characters")
	case !validEmail(s.Email) || len(s.Email) > 100:
		return errors.New("a valid email of at most 100 characters is required")
	case s.Major == "" || len(s.Major) > 50:
		return errors.New("major is required and must be at most 50 characters")
	}
	return nil
}

func jsonBytes(v any) ([]byte, error) { return json.Marshal(v) }
