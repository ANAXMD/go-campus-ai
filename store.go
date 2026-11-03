package main

import (
	"context"
	"database/sql"
	"errors"
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
// without a running Postgres.
type Store interface {
	Ping(ctx context.Context) error
	ListStudents(ctx context.Context) ([]Student, error)
	CreateStudent(ctx context.Context, s Student) (Student, error)
	UpdateStudent(ctx context.Context, id int, s Student) (Student, error)
	DeleteStudent(ctx context.Context, id int) error
	CreateUser(ctx context.Context, email, passwordHash string) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
}

// Cache is a best-effort byte cache. Failures never fail a request: the
// server logs them and falls back to the database.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
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

func (p *PGStore) ListStudents(ctx context.Context) ([]Student, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT id, name, email, major FROM students ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	students := []Student{} // never nil, so an empty table encodes as [] and not null
	for rows.Next() {
		var s Student
		if err := rows.Scan(&s.ID, &s.Name, &s.Email, &s.Major); err != nil {
			return nil, err
		}
		students = append(students, s)
	}
	return students, rows.Err()
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

func (p *PGStore) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	u := User{Email: email, PasswordHash: passwordHash}
	err := p.db.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, passwordHash).Scan(&u.ID)
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	return u, err
}

func (p *PGStore) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := p.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
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

func (r *RedisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// NopCache is used when Redis is unavailable: every read misses and every
// write is discarded.
type NopCache struct{}

func (NopCache) Get(context.Context, string) ([]byte, bool)               { return nil, false }
func (NopCache) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (NopCache) Delete(context.Context, string) error                     { return nil }
