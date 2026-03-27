package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// PostgresConfig holds the connection parameters for a Postgres database.
type PostgresConfig struct {
	URL           string
	URLFile       string
	VectorEnabled bool
}

const (
	defaultMaxTimeout   = 120 * time.Second
	defaultInitialDelay = 500 * time.Millisecond
	defaultMaxDelay     = 5 * time.Second
)

// Connect opens a Postgres connection using cfg, resolving the URL from a file
// if URLFile is set, and retries PingContext with exponential backoff until the
// connection succeeds or defaultMaxTimeout elapses.
func Connect(ctx context.Context, cfg *PostgresConfig) (*sql.DB, error) {
	url := cfg.URL
	if cfg.URLFile != "" {
		resolved, err := resolveURLFile(cfg.URLFile)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve postgres URL from file: %w", err)
		}
		url = resolved
	}
	return retryDBConnection(ctx, url)
}

// retryDBConnection opens a database connection and retries PingContext with
// exponential backoff until the connection succeeds or defaultMaxTimeout elapses.
func retryDBConnection(ctx context.Context, url string) (*sql.DB, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultMaxTimeout)
	defer cancel()

	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	start := time.Now()
	delay := defaultInitialDelay
	for attempt := 1; ; attempt++ {
		if err := db.PingContext(ctx); err == nil {
			return db, nil
		} else {
			log.Printf("database not ready (attempt %d, elapsed %s): %v", attempt, time.Since(start).Round(time.Second), err)
		}
		select {
		case <-ctx.Done():
			_ = db.Close()
			return nil, fmt.Errorf("database not ready after %s: %w", time.Since(start).Round(time.Second), ctx.Err())
		case <-time.After(delay):
		}
		delay *= 2
		if delay > defaultMaxDelay {
			delay = defaultMaxDelay
		}
	}
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
