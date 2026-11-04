package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests need real services, so they only run when told where they are:
//
//	TEST_DATABASE_URL  postgres://user:pass@localhost:5432/postgres?sslmode=disable
//	TEST_REDIS_ADDR    localhost:6379
//
// Each Postgres test gets its own throwaway database, created and dropped by
// the test, so the URL only needs to point at any database on a server where
// the user may create databases. Locally they are skipped when the variables
// are unset. On CI (CI=true) a missing variable is a failure, so a broken
// workflow can never quietly stop running them.

func integrationEnv(t *testing.T, name string) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s is not set, but this is CI: the integration tests must run, not be skipped", name)
	}
	t.Skipf("%s is not set; skipping the integration test (see README, Testing)", name)
	return ""
}

func randomName(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(b))
}

// newEmptyDB creates a fresh, empty database and returns a connection to it.
func newEmptyDB(t *testing.T) *sql.DB {
	t.Helper()
	adminURL := integrationEnv(t, "TEST_DATABASE_URL")

	admin, err := sql.Open("postgres", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	name := randomName("campus_test")
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		admin.Close()
		t.Fatalf("create test database: %v", err)
	}

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL must be a postgres:// URL: %v", err)
	}
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(`DROP DATABASE ` + name + ` WITH (FORCE)`); err != nil {
			t.Logf("could not drop %s: %v", name, err)
		}
		admin.Close()
	})
	return db
}

// newMigratedDB is newEmptyDB with the real embedded migrations applied.
func newMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newEmptyDB(t)
	migs, err := loadMigrations(embeddedMigrations, "db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(context.Background(), db, migs); err != nil {
		t.Fatal(err)
	}
	return db
}

// ---------------------------------------------------------------- Postgres

// The same specification the in-memory fake passes, run against real Postgres.
func TestPGStoreContract(t *testing.T) {
	integrationEnv(t, "TEST_DATABASE_URL") // skip (or fail on CI) once, before any subtests
	runStoreContract(t, func(t *testing.T) Store { return NewPGStore(newMigratedDB(t)) })
}

func TestPGStoreRejectsAnUnknownRole(t *testing.T) {
	store := NewPGStore(newMigratedDB(t))
	_, err := store.CreateUser(context.Background(), "x@x.io", "h", Role("superuser"))
	if err == nil {
		t.Fatal("the database accepted a role the application does not know")
	}
	if isUniqueViolation(err) {
		t.Fatalf("err = %v, want a check-constraint failure, not a unique violation", err)
	}
}

func TestPGStoreSeesAConsistentSnapshotForAPageAndItsTotal(t *testing.T) {
	// A page and its total are read in one read-only REPEATABLE READ
	// transaction. This checks that the transaction mode is accepted by the
	// driver and server, and that a plain page and total still agree.
	store := NewPGStore(newMigratedDB(t))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := store.CreateStudent(ctx, Student{Name: fmt.Sprint("S", i), Email: fmt.Sprint("s", i, "@x.io"), Major: "Art"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListStudents(ctx, StudentQuery{Limit: 2})
	if err != nil || page.Total != 5 || len(page.Items) != 2 {
		t.Fatalf("page = %+v, %v", page, err)
	}
}

// ------------------------------------------------------------- migrations

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestMigrationsApplyOnceAndAreRepeatable(t *testing.T) {
	db := newEmptyDB(t)
	migs, err := loadMigrations(embeddedMigrations, "db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for run := 1; run <= 2; run++ {
		if err := runMigrations(ctx, db, migs); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if got := countRows(t, db, `SELECT count(*) FROM schema_migrations`); got != len(migs) {
			t.Fatalf("run %d: %d versions recorded, want %d", run, got, len(migs))
		}
	}
	for _, table := range []string{"users", "students", "schema_migrations"} {
		if countRows(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1`, table) != 1 {
			t.Errorf("table %s was not created", table)
		}
	}
}

// Several API replicas starting at the same moment must not race each other.
func TestMigrationsAreSafeWhenReplicasStartTogether(t *testing.T) {
	db := newEmptyDB(t)
	migs, err := loadMigrations(embeddedMigrations, "db/migrations")
	if err != nil {
		t.Fatal(err)
	}

	const replicas = 6
	errs := make([]error, replicas)
	var wg sync.WaitGroup
	for i := 0; i < replicas; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = runMigrations(context.Background(), db, migs)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("replica %d: %v", i, err)
		}
	}
	if got := countRows(t, db, `SELECT count(*) FROM schema_migrations`); got != len(migs) {
		t.Fatalf("%d versions recorded, want exactly %d (a migration ran twice)", got, len(migs))
	}
}

// A database created by an earlier release has users and students but no role
// column and no schema_migrations table. Starting the new release on it must
// keep every row and turn existing accounts into viewers.
func TestMigrationsUpgradeAnExistingDatabase(t *testing.T) {
	db := newEmptyDB(t)
	for _, file := range []string{"000001_create_users_table.up.sql", "000002_create_students_table.up.sql"} {
		body, err := embeddedMigrations.ReadFile("db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users (email, password_hash) VALUES ('old@x.io', 'hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO students (name, email, major) VALUES ('Old', 'old-student@x.io', 'Art')`); err != nil {
		t.Fatal(err)
	}

	migs, err := loadMigrations(embeddedMigrations, "db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(context.Background(), db, migs); err != nil {
		t.Fatal(err)
	}

	user, err := NewPGStore(db).UserByEmail(context.Background(), "old@x.io")
	if err != nil || user.Role != RoleViewer || user.PasswordHash != "hash" {
		t.Fatalf("existing user after upgrade = %+v, %v; want the same account as a viewer", user, err)
	}
	if got := countRows(t, db, `SELECT count(*) FROM students`); got != 1 {
		t.Fatalf("%d students after upgrade, want the 1 that existed", got)
	}
}

// A migration is all or nothing: if it fails halfway, none of it stays.
func TestAFailingMigrationLeavesNothingBehind(t *testing.T) {
	db := newEmptyDB(t)
	migs := []migration{
		{version: 1, name: "000001_ok.up.sql", sql: `CREATE TABLE fine (id int);`},
		{version: 2, name: "000002_broken.up.sql", sql: `CREATE TABLE half_done (id int); SELECT * FROM table_that_does_not_exist;`},
		{version: 3, name: "000003_never.up.sql", sql: `CREATE TABLE never (id int);`},
	}

	err := runMigrations(context.Background(), db, migs)
	if err == nil || !strings.Contains(err.Error(), "000002_broken.up.sql") {
		t.Fatalf("err = %v, want it to name the failing migration", err)
	}

	exists := func(table string) bool {
		return countRows(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1`, table) == 1
	}
	if !exists("fine") {
		t.Error("the migration before the failure should have been kept")
	}
	if exists("half_done") {
		t.Error("the half-applied migration was not rolled back")
	}
	if exists("never") {
		t.Error("migrations after the failure must not run")
	}
	if got := countRows(t, db, `SELECT count(*) FROM schema_migrations`); got != 1 {
		t.Fatalf("%d versions recorded, want only the first", got)
	}
}

// ------------------------------------------------------------------ Redis

func TestRedisCacheIntegration(t *testing.T) {
	addr := integrationEnv(t, "TEST_REDIS_ADDR")
	cache := NewRedisCache(addr, "")
	ctx := context.Background()
	if err := cache.Ping(ctx); err != nil {
		t.Fatalf("cannot reach redis at %s: %v", addr, err)
	}
	key := randomName("test")
	defer cache.client.Del(ctx, key)

	if v, ok := cache.Get(ctx, key); ok {
		t.Fatalf("a missing key reported a hit: %q", v)
	}

	payload := []byte("{\"items\":[],\"unicode\":\"é\"}\x00binary")
	if err := cache.Set(ctx, key, payload, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, ok := cache.Get(ctx, key); !ok || string(got) != string(payload) {
		t.Fatalf("round trip = %q, %v", got, ok)
	}

	t.Run("entries expire", func(t *testing.T) {
		short := randomName("test-ttl")
		if err := cache.Set(ctx, short, []byte("x"), 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		if _, ok := cache.Get(ctx, short); ok {
			t.Fatal("the entry outlived its TTL")
		}
	})

	t.Run("Incr counts up from nothing and the value reads back", func(t *testing.T) {
		counter := randomName("test-gen")
		defer cache.client.Del(ctx, counter)
		for want := int64(1); want <= 3; want++ {
			got, err := cache.Incr(ctx, counter)
			if err != nil || got != want {
				t.Fatalf("Incr = %d, %v; want %d", got, err, want)
			}
		}
		if raw, ok := cache.Get(ctx, counter); !ok || string(raw) != "3" {
			t.Fatalf("Get after Incr = %q, %v; the server reads the generation this way", raw, ok)
		}
	})

	t.Run("Incr on a value that is not a number is an error, not a silent reset", func(t *testing.T) {
		if _, err := cache.Incr(ctx, key); err == nil {
			t.Fatal("Incr on a non-integer succeeded")
		}
	})

	t.Run("a closed client fails safe", func(t *testing.T) {
		closed := NewRedisCache(addr, "")
		closed.Close()
		if _, ok := closed.Get(ctx, key); ok {
			t.Error("Get on a closed client reported a hit")
		}
		if err := closed.Set(ctx, key, []byte("x"), time.Minute); err == nil {
			t.Error("Set on a closed client did not fail")
		}
		if _, err := closed.Incr(ctx, "x"); err == nil {
			t.Error("Incr on a closed client did not fail")
		}
	})
}

// The whole read path, with the real Redis behind it: miss, hit, then a write
// bumps the generation and the next read is fresh. The in-memory fake cannot
// show that the real INCR and GET agree about the counter's format.
func TestCacheInvalidationAgainstRealRedis(t *testing.T) {
	addr := integrationEnv(t, "TEST_REDIS_ADDR")
	cache := NewRedisCache(addr, "")
	defer cache.Close()
	ctx := context.Background()

	defer cache.client.Del(ctx, cacheGenKey)

	// Start the generation at a number no earlier run used, so entries left
	// over from other runs can never be mistaken for ours.
	start := time.Now().UnixNano() / 1000
	if err := cache.Set(ctx, cacheGenKey, []byte(fmt.Sprint(start)), time.Hour); err != nil {
		t.Fatal(err)
	}

	h := newHarnessWith(t, map[string]string{"CACHE_TTL": "30s"})
	h.srv.cache = cache
	token := h.login()

	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Ada", "ada@campus.edu", "Math")), http.StatusCreated)
	if page, source := getPage(t, h, token, ""); source != "MISS" || page.Total != 1 {
		t.Fatalf("first read: %+v X-Cache=%s", page, source)
	}
	if _, source := getPage(t, h, token, ""); source != "HIT" {
		t.Fatalf("second read X-Cache = %s, want HIT from the real Redis", source)
	}

	wantStatus(t, h.do("POST", "/api/students", token, newStudent("Alan", "alan@campus.edu", "Math")), http.StatusCreated)
	if page, source := getPage(t, h, token, ""); source != "MISS" || page.Total != 2 {
		t.Fatalf("after a write: %+v X-Cache=%s, want a fresh MISS", page, source)
	}
	if raw, _ := cache.Get(ctx, cacheGenKey); string(raw) != fmt.Sprint(start+2) {
		t.Fatalf("generation = %s, want %d (one bump per successful write)", raw, start+2)
	}
}
