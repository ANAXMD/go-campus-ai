package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Fatalf("fatal: %v", err)
	}
}

func run(logger *log.Logger) error {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := openDB(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer db.Close()

	migs, err := loadMigrations(embeddedMigrations, "db/migrations")
	if err != nil {
		return err
	}
	if err := runMigrations(ctx, db, migs); err != nil {
		return err
	}

	// The ADMIN_EMAILS setting is the source of truth for roles; apply it now so
	// accounts that already exist are promoted or demoted to match.
	store := NewPGStore(db)
	if err := store.SyncAdmins(ctx, cfg.AdminEmails); err != nil {
		return fmt.Errorf("apply ADMIN_EMAILS: %w", err)
	}
	if len(cfg.AdminEmails) == 0 {
		logger.Println("WARNING: ADMIN_EMAILS is empty, so nobody can change the directory (everyone is a read-only viewer)")
	} else {
		logger.Printf("administrators configured: %d", len(cfg.AdminEmails))
	}

	// Redis is an optimisation, not a dependency: if it is unreachable the API
	// still works, just without caching.
	var cache Cache = NopCache{}
	rc := NewRedisCache(cfg.RedisAddr, cfg.RedisPassword)
	if err := rc.Ping(ctx); err != nil {
		logger.Printf("redis unavailable at %s, running without cache: %v", cfg.RedisAddr, err)
		rc.Close()
	} else {
		logger.Printf("redis connected at %s", cfg.RedisAddr)
		cache = rc
		defer rc.Close()
	}

	srv, err := NewServer(cfg, store, cache, logger)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s", httpSrv.Addr)
		serveErr <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Println("shutting down...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	srv.Wait() // let in-flight welcome emails finish
	logger.Println("stopped cleanly")
	return nil
}

// openDB connects to Postgres, retrying for a while because in Docker the
// database often accepts connections a few seconds after the API starts.
func openDB(ctx context.Context, url string, logger *log.Logger) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	deadline := time.Now().Add(30 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			logger.Println("database connected")
			return db, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			db.Close()
			return nil, errors.Join(errors.New("database unreachable"), err)
		}
		logger.Printf("waiting for database: %v", err)
		time.Sleep(time.Second)
	}
}
