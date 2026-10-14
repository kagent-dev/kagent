package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvectorpgx "github.com/pgvector/pgvector-go/pgx"

	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// PostgresConfig holds the connection parameters for a Postgres database.
// Pool is the login's base configuration from postgres.PoolConfig; Connect
// copies it and never modifies it.
// Pool fields are optional: nil leaves the corresponding pgxpool.Config value
// from ParseConfig unchanged (pgx library defaults).
// Schema is required when vectors are enabled; VectorSchema defaults to extensions.
type PostgresConfig struct {
	Pool            *pgxpool.Config
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

func poolConfig(cfg *PostgresConfig) (*pgxpool.Config, error) {
	config := cfg.Pool.Copy()
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

	if cfg.VectorEnabled || cfg.Schema != "" {
		assumeRole := config.AfterConnect
		config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if err := assumeRole(ctx, conn); err != nil {
				return err
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
