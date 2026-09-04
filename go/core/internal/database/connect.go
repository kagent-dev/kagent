package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kagent-dev/kagent/go/internal/logging"
	pgvectorpgx "github.com/pgvector/pgvector-go/pgx"
)

// PostgresConfig holds the connection parameters for a Postgres database.
// URL must be a resolved connection string — use ResolveURL to resolve from
// a file path before constructing this config.
//
// Pool fields are optional: nil leaves the corresponding pgxpool.Config value
// from ParseConfig unchanged (pgx library defaults).
type PostgresConfig struct {
	URL             string
	VectorEnabled   bool
	MaxConns        *int32
	MinConns        *int32
	MaxConnIdleTime *time.Duration
	MaxConnLifetime *time.Duration
}

const (
	defaultMaxTimeout   = 120 * time.Second
	defaultInitialDelay = 500 * time.Millisecond
	defaultMaxDelay     = 5 * time.Second
)

// Connect opens a Postgres connection pool using cfg and retries Ping with
// exponential backoff until the connection succeeds or defaultMaxTimeout elapses.
func Connect(ctx context.Context, cfg *PostgresConfig) (*pgxpool.Pool, error) {
	return retryDBConnection(ctx, cfg)
}

// applyPoolConfig copies non-nil pool settings from cfg onto config and
// validates the resulting pool bounds.
func applyPoolConfig(config *pgxpool.Config, cfg *PostgresConfig) error {
	if cfg.MaxConns != nil {
		config.MaxConns = *cfg.MaxConns
	}
	if cfg.MinConns != nil {
		config.MinConns = *cfg.MinConns
	}
	if cfg.MaxConnIdleTime != nil {
		config.MaxConnIdleTime = *cfg.MaxConnIdleTime
	}
	if cfg.MaxConnLifetime != nil {
		config.MaxConnLifetime = *cfg.MaxConnLifetime
	}
	if config.MaxConns < 1 {
		return fmt.Errorf("db maxConns must be >= 1, got %d", config.MaxConns)
	}
	if config.MinConns > config.MaxConns {
		return fmt.Errorf("db minConns (%d) cannot be greater than maxConns (%d)", config.MinConns, config.MaxConns)
	}
	return nil
}

// retryDBConnection opens a pgxpool connection, registering pgvector types when
// vectorEnabled is true, and retries Ping with exponential backoff until the
// connection succeeds or defaultMaxTimeout elapses.
func retryDBConnection(ctx context.Context, cfg *PostgresConfig) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultMaxTimeout)
	defer cancel()

	config, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}
	if err := applyPoolConfig(config, cfg); err != nil {
		return nil, err
	}
	if cfg.VectorEnabled {
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
			logging.FromContext(ctx).WarnContext(ctx, "database not ready", "error", err, "attempt", attempt, "elapsed_ms", time.Since(start).Milliseconds())
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
