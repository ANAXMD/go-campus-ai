package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// The migrations are compiled into the binary, so the container needs no
// extra files and the schema always matches the code that is running. The
// NNNNNN_name.up.sql naming is compatible with golang-migrate should this
// ever outgrow the small runner below.
//
//go:embed db/migrations/*.up.sql
var embeddedMigrations embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads every *.up.sql file in dir, ordered by version.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}

	var migs []migration
	seen := map[int]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		version, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil || version <= 0 {
			return nil, fmt.Errorf("migration %q: file name must look like 000001_description.up.sql", name)
		}
		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations %q and %q share version %d", other, name, version)
		}
		seen[version] = name

		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, err
		}
		migs = append(migs, migration{version: version, name: name, sql: string(body)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}

// migrationLockID is an arbitrary constant used as a Postgres advisory-lock
// key so that two API replicas starting together do not race each other.
const migrationLockID = 727274

// runMigrations applies every migration that has not run yet, each in its own
// transaction. It is safe to call on every startup and from several replicas.
func runMigrations(ctx context.Context, db *sql.DB, migs []migration) error {
	for _, m := range migs {
		if err := applyMigration(ctx, db, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	var applied bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.version).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version); err != nil {
		return err
	}
	return tx.Commit()
}
