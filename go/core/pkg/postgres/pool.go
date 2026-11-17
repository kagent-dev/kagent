// Package postgres builds kagent's PostgreSQL connection pool configuration.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errInvalidURL = errors.New("invalid PostgreSQL connection string")

// dialTimeout bounds the TCP dial when the URL sets no connect_timeout. It does
// not bound the login, which a resuming database can hold for tens of seconds.
const dialTimeout = 10 * time.Second

// PoolConfig parses url into a pool configuration. Every connection assumes
// role after authentication, so the login only needs membership in role.
// Errors never include url, which can carry a password.
func PoolConfig(url, role string) (*pgxpool.Config, error) {
	if role == "" {
		return nil, errors.New("PostgreSQL role must not be empty")
	}
	config, err := parse(url)
	if err != nil {
		return nil, err
	}
	if config.ConnConfig.ConnectTimeout == 0 {
		config.ConnConfig.DialFunc = (&net.Dialer{Timeout: dialTimeout}).DialContext
	}
	if usesTLS(config.ConnConfig) {
		// pgx reads TLS files once at parse time. Reparse before each new physical
		// connection so rotated client certificates take effect without a restart.
		config.BeforeConnect = func(_ context.Context, connConfig *pgx.ConnConfig) error {
			fresh, err := parse(url)
			if err != nil {
				return err
			}
			refreshed := fresh.ConnConfig.Config.Copy()
			connConfig.TLSConfig = refreshed.TLSConfig
			connConfig.Fallbacks = refreshed.Fallbacks
			return nil
		}
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SELECT set_config('role', $1, false)", role); err != nil {
			return fmt.Errorf("assuming PostgreSQL role %q: %w", role, err)
		}
		return nil
	}
	return config, nil
}

func parse(url string) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		// pgx parse errors retain the input string, so wrapping err here could
		// disclose the password read from a Secret.
		return nil, fmt.Errorf("parse database connection source: %w", errInvalidURL)
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
