package main

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

var (
	// ErrNotFound means the row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a unique constraint (an email) was violated.
	ErrConflict = errors.New("conflict")
)

// Store is everything the HTTP layer needs from persistent storage. The
// handlers depend on this interface, not on *sql.DB, so they can be tested
// without a running Postgres. store_contract_test.go runs one suite against
// both the in-memory fake and the real Postgres implementation, which is what
// keeps the fake honest.
type Store interface {
	Ping(ctx context.Context) error

	ListStudents(ctx context.Context, q StudentQuery) (StudentPage, error)
	StudentStats(ctx context.Context) (StudentStats, error)
	CreateStudent(ctx context.Context, s Student) (Student, error)
	UpdateStudent(ctx context.Context, id int, s Student) (Student, error)
	DeleteStudent(ctx context.Context, id int) error

	CreateUser(ctx context.Context, email, passwordHash string, role Role) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	// SyncAdmins makes exactly the listed addresses admins and everyone else a
	// viewer, in one transaction. Addresses with no account yet are ignored.
	SyncAdmins(ctx context.Context, emails []string) error
}

// Cache is a best-effort byte cache. Failures never fail a request: the
// server logs them and falls back to the database.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Incr atomically adds one to an integer key (creating it at 1) and
	// returns the new value. The server uses it as a version counter.
	Incr(ctx context.Context, key string) (int64, error)
}

// ---------------------------------------------------------------- Postgres

// PGStore implements Store on top of database/sql.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

const pgUniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == pgUniqueViolation
}

func (p *PGStore) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

// readOnlySnapshot runs fn in a read-only REPEATABLE READ transaction, so two
// queries that belong together (a page and its total) see the same data even
// while other requests are writing.
func (p *PGStore) readOnlySnapshot(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // nothing to undo for a read-only transaction
	return fn(tx)
}

// likePattern turns user input into a contains-pattern for ILIKE. The
// characters % and _ (and the escape character itself) are wildcards in LIKE,
// so they are escaped: searching for "100%" finds "100%", not everything.
func likePattern(search string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(search) + "%"
}

func (p *PGStore) ListStudents(ctx context.Context, q StudentQuery) (StudentPage, error) {
	page := StudentPage{Items: []Student{}, Limit: q.Limit, Offset: q.Offset} // never nil: an empty page encodes as [] not null

	where := ""
	var args []any
	if q.Search != "" {
		where = ` WHERE name ILIKE $1 ESCAPE '\' OR email ILIKE $1 ESCAPE '\' OR major ILIKE $1 ESCAPE '\'`
		args = append(args, likePattern(q.Search))
	}

	err := p.readOnlySnapshot(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM students`+where, args...).Scan(&page.Total); err != nil {
			return err
		}

		limitArg, offsetArg := "$"+strconv.Itoa(len(args)+1), "$"+strconv.Itoa(len(args)+2)
		rows, err := tx.QueryContext(ctx,
			`SELECT id, name, email, major FROM students`+where+` ORDER BY id LIMIT `+limitArg+` OFFSET `+offsetArg,
			append(args, q.Limit, q.Offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s Student
			if err := rows.Scan(&s.ID, &s.Name, &s.Email, &s.Major); err != nil {
				return err
			}
			page.Items = append(page.Items, s)
		}
		return rows.Err()
	})
	return page, err
}

func (p *PGStore) StudentStats(ctx context.Context) (StudentStats, error) {
	var st StudentStats
	err := p.readOnlySnapshot(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*), count(DISTINCT major) FROM students`).Scan(&st.Total, &st.Majors); err != nil {
			return err
		}
		// COLLATE "C" makes ties break by plain byte order, whatever the
		// database's locale is, so the answer is the same on every machine.
		err := tx.QueryRowContext(ctx,
			`SELECT major, count(*) AS n FROM students GROUP BY major ORDER BY n DESC, major COLLATE "C" LIMIT 1`).
			Scan(&st.TopMajor, &st.TopCount)
		if errors.Is(err, sql.ErrNoRows) {
			return nil // empty table: TopMajor "" and TopCount 0
		}
		return err
	})
	return st, err
}

func (p *PGStore) CreateStudent(ctx context.Context, s Student) (Student, error) {
	err := p.db.QueryRowContext(ctx,
		`INSERT INTO students (name, email, major) VALUES ($1, $2, $3) RETURNING id`,
		s.Name, s.Email, s.Major).Scan(&s.ID)
	if isUniqueViolation(err) {
		return Student{}, ErrConflict
	}
	return s, err
}

func (p *PGStore) UpdateStudent(ctx context.Context, id int, s Student) (Student, error) {
	res, err := p.db.ExecContext(ctx,
		`UPDATE students SET name = $1, email = $2, major = $3 WHERE id = $4`,
		s.Name, s.Email, s.Major, id)
	if isUniqueViolation(err) {
		return Student{}, ErrConflict
	}
	if err != nil {
		return Student{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Student{}, err
	} else if n == 0 {
		return Student{}, ErrNotFound
	}
	s.ID = id
	return s, nil
}

func (p *PGStore) DeleteStudent(ctx context.Context, id int) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM students WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PGStore) CreateUser(ctx context.Context, email, passwordHash string, role Role) (User, error) {
	u := User{Email: email, PasswordHash: passwordHash, Role: role}
	err := p.db.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3) RETURNING id`,
		email, passwordHash, string(role)).Scan(&u.ID)
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	return u, err
}

func (p *PGStore) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	var role string
	err := p.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.Role = Role(role)
	return u, err
}

func (p *PGStore) SyncAdmins(ctx context.Context, emails []string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = 'viewer' WHERE role = 'admin'`); err != nil {
		return err
	}
	for _, email := range emails {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET role = 'admin' WHERE email = $1`, email); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ------------------------------------------------------------------- Redis

// RedisCache implements Cache on top of go-redis.
type RedisCache struct{ client *redis.Client }

func NewRedisCache(addr, password string) *RedisCache {
	return &RedisCache{client: redis.NewClient(&redis.Options{Addr: addr, Password: password})}
}

func (r *RedisCache) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }
func (r *RedisCache) Close() error                   { return r.client.Close() }

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err != nil { // redis.Nil (a plain miss) and real errors both fall through to the database
		return nil, false
	}
	return val, true
}

func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisCache) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

// NopCache is used when Redis is unavailable: every read misses and every
// write is discarded.
type NopCache struct{}

func (NopCache) Get(context.Context, string) ([]byte, bool)               { return nil, false }
func (NopCache) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (NopCache) Incr(context.Context, string) (int64, error)              { return 0, nil }
