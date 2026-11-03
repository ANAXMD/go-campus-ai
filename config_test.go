package main

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(mapEnv(map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": testSecret}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || cfg.RedisAddr != "localhost:6379" || cfg.CacheTTL != 10*time.Minute ||
		cfg.TokenTTL != 24*time.Hour || len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "http://localhost:3000" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	cfg, err := LoadConfig(mapEnv(map[string]string{
		"DATABASE_URL":         "postgres://x",
		"JWT_SECRET":           testSecret,
		"PORT":                 "9000",
		"CORS_ALLOWED_ORIGINS": " https://a.example , https://b.example ,, ",
		"CACHE_TTL":            "30s",
		"WELCOME_EMAIL_DELAY":  "0s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9000" || cfg.CacheTTL != 30*time.Second || cfg.EmailDelay != 0 {
		t.Fatalf("overrides ignored: %+v", cfg)
	}
	if got := strings.Join(cfg.AllowedOrigins, "|"); got != "https://a.example|https://b.example" {
		t.Fatalf("origins = %q", got)
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"nothing set", map[string]string{}, "DATABASE_URL is required"},
		{"no secret", map[string]string{"DATABASE_URL": "x"}, "JWT_SECRET"},
		{"short secret", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": "too-short"}, "at least 32"},
		{"bad duration", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": testSecret, "CACHE_TTL": "ten minutes"}, "CACHE_TTL"},
		{"negative duration", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": testSecret, "TOKEN_TTL": "-1h"}, "TOKEN_TTL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(mapEnv(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigReportsEveryProblemAtOnce(t *testing.T) {
	_, err := LoadConfig(mapEnv(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("err = %v, want both problems listed", err)
	}
}

func TestLoadMigrationsOrdersByVersionAndSkipsDownFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"m/000002_b.up.sql":   {Data: []byte("SELECT 2;")},
		"m/000001_a.up.sql":   {Data: []byte("SELECT 1;")},
		"m/000001_a.down.sql": {Data: []byte("DROP TABLE a;")},
		"m/README.md":         {Data: []byte("ignored")},
	}
	migs, err := loadMigrations(fsys, "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 2 || migs[0].version != 1 || migs[1].version != 2 || migs[1].sql != "SELECT 2;" {
		t.Fatalf("migs = %+v", migs)
	}
}

func TestLoadMigrationsRejectsBadNames(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"no version":        {"m/create_users.up.sql": {Data: []byte("x")}},
		"zero version":      {"m/000000_x.up.sql": {Data: []byte("x")}},
		"duplicate version": {"m/1_a.up.sql": {Data: []byte("x")}, "m/000001_b.up.sql": {Data: []byte("x")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadMigrations(files, "m"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// The real embedded migrations must parse and cover both tables the API uses.
func TestEmbeddedMigrationsAreValid(t *testing.T) {
	migs, err := loadMigrations(embeddedMigrations, "db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	all := ""
	for _, m := range migs {
		all += m.sql
	}
	for _, table := range []string{"users", "students"} {
		if !strings.Contains(all, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("no migration creates the %s table", table)
		}
	}
}
