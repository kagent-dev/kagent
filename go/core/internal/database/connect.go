package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvectorpgx "github.com/pgvector/pgvector-go/pgx"
)

// PostgresConfig holds the connection parameters for a Postgres database.
// URL must be a resolved connection string — use ResolveURL to resolve from
// a file path before constructing this config.
type PostgresConfig struct {
	URL           string
	VectorEnabled bool
}

const (
	defaultMaxTimeout   = 120 * time.Second
	defaultInitialDelay = 500 * time.Millisecond
	defaultMaxDelay     = 5 * time.Second
)

// Connect opens a Postgres connection pool using cfg and retries Ping with
// exponential backoff until the connection succeeds or defaultMaxTimeout elapses.
func Connect(ctx context.Context, cfg *PostgresConfig) (*pgxpool.Pool, error) {
	return retryDBConnection(ctx, cfg.URL, cfg.VectorEnabled)
}

// retryDBConnection opens a pgxpool connection, registering pgvector types when
// vectorEnabled is true, and retries Ping with exponential backoff until the
// connection succeeds or defaultMaxTimeout elapses.
func retryDBConnection(ctx context.Context, url string, vectorEnabled bool) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultMaxTimeout)
	defer cancel()

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}
	if vectorEnabled {
		config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			return pgvectorpgx.RegisterTypes(ctx, conn)
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create database pool: %w", err)
	}

	start := time.Now()
	delay := defaultInitialDelay
	for attempt := 1; ; attempt++ {
		if err := pool.Ping(ctx); err == nil {
			return pool, nil
		} else {
			log.Printf("database not ready (attempt %d, elapsed %s): %v", attempt, time.Since(start).Round(time.Second), err)
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("database not ready after %s: %w", time.Since(start).Round(time.Second), ctx.Err())
		case <-time.After(delay):
		}
		delay *= 2
		if delay > defaultMaxDelay {
			delay = defaultMaxDelay
		}
	}
}

// ResolveURL returns url, unless urlFile is non-empty in which case the URL is
// read from that file. Used by callers (e.g. the migration runner) that need
// the resolved connection string before a pool is created.
func ResolveURL(url, urlFile string) (string, error) {
	if urlFile != "" {
		return resolveURLFile(urlFile)
	}
	return url, nil
}

// resolveURLFile reads a database connection URL from a file and returns the
// trimmed contents. Returns an error if the file cannot be read or is empty.
func resolveURLFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading URL file: %w", err)
	}
	url := strings.TrimSpace(string(content))
	if url == "" {
		return "", fmt.Errorf("URL file %s is empty or contains only whitespace", path)
	}
	return url, nil
}
