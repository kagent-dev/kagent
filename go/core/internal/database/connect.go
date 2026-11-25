package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// Connect returns a PostgreSQL pool after a successful ping. Transient startup
// failures retry until the context is canceled or two minutes elapse.
// VectorEnabled registers pgvector types on each connection. The caller closes the pool.
func Connect(ctx context.Context, cfg *PostgresConfig) (*pgxpool.Pool, error) {
	config, err := poolConfig(cfg)
	if err != nil {
		return nil, err
	}
	return connectPool(ctx, config)
}

// WaitForPostgres checks connectivity with the caller's login and role before
// migration or verification. It does not require the runtime schema or types.
func WaitForPostgres(ctx context.Context, config *pgxpool.Config) error {
	pool, err := connectPool(ctx, config)
	if err != nil {
		return err
	}
	pool.Close()
	return nil
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

// connectPool closes the pool if startup fails and transfers ownership on success.
func connectPool(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create database pool: %w", err)
	}
	if err := waitForPing(ctx, pool.Ping); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// waitForPing bounds startup even when the peer accepts TCP but stalls during login.
func waitForPing(ctx context.Context, ping func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, defaultMaxTimeout)
	defer cancel()
	start := time.Now()
	delay := defaultInitialDelay
	var lastErr error
	for attempt := 1; ctx.Err() == nil; attempt++ {
		err := ping(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			if lastErr == nil || !errors.Is(err, ctx.Err()) {
				lastErr = err
			}
			break
		}
		if !isTransientStartupError(err) {
			return err
		}
		lastErr = err
		logging.FromContext(ctx).WarnContext(ctx, "waiting for PostgreSQL", "error", err, "attempt", attempt, "elapsed_ms", time.Since(start).Milliseconds())
		select {
		case <-ctx.Done():
		case <-time.After(delay):
			delay = min(2*delay, defaultMaxDelay)
		}
	}
	return fmt.Errorf("PostgreSQL unavailable after %s: %w", time.Since(start).Round(time.Millisecond), errors.Join(lastErr, ctx.Err()))
}

func isTransientStartupError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "57P03" // cannot_connect_now
	}
	var dnsErr *net.DNSError
	var netErr net.Error
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.As(err, &dnsErr) ||
		pgconn.Timeout(err) ||
		(errors.As(err, &netErr) && netErr.Timeout())
}
