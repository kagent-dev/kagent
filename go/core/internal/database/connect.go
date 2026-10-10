package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	pgvectorpgx "github.com/pgvector/pgvector-go/pgx"
)

// PostgresConfig holds the connection parameters for a Postgres database.
// URL is a PostgreSQL connection string.
// Pool fields are optional: nil leaves the corresponding pgxpool.Config value
// from ParseConfig unchanged (pgx library defaults).
// Role, when set, is assumed on every connection after authentication.
// Schema is required when vectors are enabled; VectorSchema defaults to extensions.
type PostgresConfig struct {
	URL             string
	Role            string
	Schema          string
	VectorSchema    string
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

var errInvalidDatabaseURL = errors.New("invalid PostgreSQL connection string")

// Connect returns a PostgreSQL pool after a successful ping, retrying until the
// context is canceled or two minutes elapse. Invalid configuration fails immediately.
// VectorEnabled registers pgvector types on each connection. The caller closes the pool.
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

// ValidateURL validates a PostgreSQL connection string without disclosing it in errors.
func ValidateURL(url string) error {
	_, err := parsePoolConfig(url)
	return err
}

func parsePoolConfig(url string) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		// pgx parse errors retain the input string, so wrapping err here could
		// disclose the password read from a Secret.
		return nil, fmt.Errorf("parse database connection source: %w", errInvalidDatabaseURL)
	}
	return config, nil
}

func poolConfig(cfg *PostgresConfig) (*pgxpool.Config, error) {
	config, err := parsePoolConfig(cfg.URL)
	if err != nil {
		return nil, err
	}
	if err := applyPoolConfig(config, cfg); err != nil {
		return nil, err
	}
	vectorSchema := cfg.VectorSchema
	if vectorSchema == "" {
		vectorSchema = consts.DefaultPgvectorSchema
	}
	if cfg.VectorEnabled && cfg.Schema == "" {
		return nil, errors.New("database schema is required when pgvector is enabled")
	}
	var searchPath string
	if cfg.Schema != "" {
		searchPath = pgx.Identifier{cfg.Schema}.Sanitize()
		config.ConnConfig.RuntimeParams["search_path"] = searchPath
	}

	if usesTLS(config.ConnConfig) {
		// pgx reads TLS files once at parse time. Reparse before each new physical
		// connection so rotated client certificates take effect without a restart.
		config.BeforeConnect = func(_ context.Context, connConfig *pgx.ConnConfig) error {
			fresh, err := parsePoolConfig(cfg.URL)
			if err != nil {
				return err
			}
			refreshed := fresh.ConnConfig.Config.Copy()
			connConfig.TLSConfig = refreshed.TLSConfig
			connConfig.Fallbacks = refreshed.Fallbacks
			return nil
		}
	}

	if cfg.Role != "" || cfg.VectorEnabled || cfg.Schema != "" {
		config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if cfg.Role != "" {
				if _, err := conn.Exec(ctx, "SELECT set_config('role', $1, false)", cfg.Role); err != nil {
					return fmt.Errorf("assuming PostgreSQL role %q: %w", cfg.Role, err)
				}
			}
			if cfg.Schema != "" {
				var currentSchema string
				if err := conn.QueryRow(ctx, "SELECT COALESCE(current_schema(), '')").Scan(&currentSchema); err != nil {
					return fmt.Errorf("check PostgreSQL schema %q: %w", cfg.Schema, err)
				}
				if currentSchema != cfg.Schema {
					return fmt.Errorf("PostgreSQL schema %q is not accessible (current schema is %q)", cfg.Schema, currentSchema)
				}
			}
			if cfg.VectorEnabled {
				if cfg.Schema != "" && vectorSchema != cfg.Schema {
					if _, err := conn.Exec(ctx, "SELECT set_config('search_path', $1, false)", pgx.Identifier{vectorSchema}.Sanitize()); err != nil {
						return fmt.Errorf("select pgvector schema %q: %w", vectorSchema, err)
					}
				}
				if err := pgvectorpgx.RegisterTypes(ctx, conn); err != nil {
					return err
				}
				if cfg.Schema != "" && vectorSchema != cfg.Schema {
					if _, err := conn.Exec(ctx, "SELECT set_config('search_path', $1, false)", searchPath); err != nil {
						return fmt.Errorf("restore PostgreSQL search path: %w", err)
					}
				}
				return nil
			}
			return nil
		}
	}
	return config, nil
}

func usesTLS(config *pgx.ConnConfig) bool {
	if config.TLSConfig != nil {
		return true
	}
	return slices.ContainsFunc(config.Fallbacks, func(fallback *pgconn.FallbackConfig) bool {
		return fallback.TLSConfig != nil
	})
}

// retryDBConnection opens and verifies a pool, registering vector types when enabled.
// Failed pings retry with exponential backoff until cancellation or the two-minute
// timeout; an unsuccessful pool is closed before returning the error.
func retryDBConnection(ctx context.Context, cfg *PostgresConfig) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultMaxTimeout)
	defer cancel()

	config, err := poolConfig(cfg)
	if err != nil {
		return nil, err
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
