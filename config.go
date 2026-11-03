package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config holds everything the process needs from its environment. Nothing
// secret has a default: a missing JWT_SECRET or DATABASE_URL is a startup
// error, not a silent fallback to a value that is public on GitHub.
type Config struct {
	Port           string
	DatabaseURL    string
	RedisAddr      string
	RedisPassword  string
	JWTSecret      []byte
	AllowedOrigins []string
	CacheTTL       time.Duration
	TokenTTL       time.Duration
	EmailDelay     time.Duration
}

const minSecretLen = 32

// LoadConfig reads configuration through getenv so tests can supply a map
// instead of mutating the real process environment.
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:          valueOr(getenv("PORT"), "8080"),
		DatabaseURL:   getenv("DATABASE_URL"),
		RedisAddr:     valueOr(getenv("REDIS_ADDR"), "localhost:6379"),
		RedisPassword: getenv("REDIS_PASSWORD"),
		JWTSecret:     []byte(getenv("JWT_SECRET")),
		CacheTTL:      10 * time.Minute,
		TokenTTL:      24 * time.Hour,
		EmailDelay:    3 * time.Second,
	}

	var problems []error
	if cfg.DatabaseURL == "" {
		problems = append(problems, errors.New("DATABASE_URL is required"))
	}
	if len(cfg.JWTSecret) < minSecretLen {
		problems = append(problems, fmt.Errorf("JWT_SECRET is required and must be at least %d characters (try: openssl rand -hex 32)", minSecretLen))
	}

	for _, origin := range strings.Split(valueOr(getenv("CORS_ALLOWED_ORIGINS"), "http://localhost:3000"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, origin)
		}
	}

	for _, d := range []struct {
		name string
		dst  *time.Duration
	}{
		{"CACHE_TTL", &cfg.CacheTTL},
		{"TOKEN_TTL", &cfg.TokenTTL},
		{"WELCOME_EMAIL_DELAY", &cfg.EmailDelay},
	} {
		raw := getenv(d.name)
		if raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			problems = append(problems, fmt.Errorf("%s must be a non-negative duration like 10m or 3s, got %q", d.name, raw))
			continue
		}
		*d.dst = parsed
	}

	if len(problems) > 0 {
		return Config{}, errors.Join(problems...)
	}
	return cfg, nil
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
