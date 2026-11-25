package database

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/pkg/postgres"
)

func TestConnectReportsRefusedConnectionAtDeadline(t *testing.T) {
	// Given a PostgreSQL address that refuses connections, when startup runs
	// out of time, the error includes both the deadline and the failed dial.
	config, err := pgxpool.ParseConfig("postgres://user@" + dbtest.RefuseConnections(t) + "/nodb?sslmode=disable")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	_, err = Connect(ctx, &PostgresConfig{Pool: config})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorIs(t, err, syscall.ECONNREFUSED)
}

func TestPostgresStartupRetry(t *testing.T) {
	scenarios := []struct {
		name string
		err  error
	}{
		{"Given PostgreSQL is initializing, when a ping returns 57P03 then succeeds, assert that startup continues", &pgconn.PgError{Code: "57P03"}},
		{"Given DNS is unavailable, when a ping fails then resolves, assert that startup continues", &net.DNSError{IsNotFound: true}},
		{"Given a slow network, when a ping times out then succeeds, assert that startup continues", os.ErrDeadlineExceeded},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				attempts := 0
				err := waitForPing(t.Context(), func(context.Context) error {
					attempts++
					if attempts == 1 {
						return scenario.err
					}
					return nil
				})
				require.NoError(t, err)
				assert.Equal(t, 2, attempts)
			})
		})
	}

	t.Run("Given a stalled login, when startup expires, assert that the previous connection error is retained", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			err := waitForPing(t.Context(), func(ctx context.Context) error {
				if time.Now().Equal(start) {
					return syscall.ECONNREFUSED
				}
				<-ctx.Done()
				return ctx.Err()
			})
			assert.ErrorIs(t, err, context.DeadlineExceeded)
			assert.ErrorIs(t, err, syscall.ECONNREFUSED)
			assert.Equal(t, 2*time.Minute, time.Since(start))
		})
	})

	t.Run("Given a missing schema, when the runtime pool checks it, assert that startup returns the error immediately", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			schemaErr := errors.New("PostgreSQL schema is not accessible")
			start := time.Now()
			err := waitForPing(t.Context(), func(context.Context) error { return schemaErr })
			assert.ErrorIs(t, err, schemaErr)
			assert.Equal(t, start, time.Now())
		})
	})
}

func TestApplyPoolConfig(t *testing.T) {
	base, err := pgxpool.ParseConfig("postgres://user:pass@localhost:5432/db")
	require.NoError(t, err)

	t.Run("unset leaves pgx defaults", func(t *testing.T) {
		config := base.Copy()
		applyPoolConfig(config, &PostgresConfig{})
		assert.Equal(t, base.MaxConns, config.MaxConns)
		assert.Equal(t, int32(0), config.MinConns)
		assert.Equal(t, 30*time.Minute, config.MaxConnIdleTime)
		assert.Equal(t, time.Hour, config.MaxConnLifetime)
	})

	t.Run("set fields override", func(t *testing.T) {
		config := base.Copy()
		maxConns := int32(8)
		minConns := int32(0)
		idle := time.Minute
		lifetime := 10 * time.Minute
		applyPoolConfig(config, &PostgresConfig{
			MaxConns:        &maxConns,
			MinConns:        &minConns,
			MaxConnIdleTime: &idle,
			MaxConnLifetime: &lifetime,
		})
		assert.Equal(t, int32(8), config.MaxConns)
		assert.Equal(t, int32(0), config.MinConns)
		assert.Equal(t, time.Minute, config.MaxConnIdleTime)
		assert.Equal(t, 10*time.Minute, config.MaxConnLifetime)
	})
}

func TestPoolConfigSetsSchema(t *testing.T) {
	config, err := poolConfig(&PostgresConfig{
		Pool:   testPoolConfig(t, "postgres://user:password@database:5432/app?sslmode=disable"),
		Schema: "kagent",
	})
	require.NoError(t, err)
	assert.Equal(t, `"kagent"`, config.ConnConfig.RuntimeParams["search_path"])
}

func TestPoolConfigRejectsMissingRuntimeSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skip the PostgreSQL test in short mode")
	}
	const schema = "missing_kagent_schema_for_connect_test"
	config, err := poolConfig(&PostgresConfig{Pool: dbtest.PoolConfig(sharedConnStr), Schema: schema})
	require.NoError(t, err)
	conn, err := pgx.ConnectConfig(t.Context(), config.ConnConfig.Copy())
	require.NoError(t, err)
	defer conn.Close(t.Context())

	err = config.AfterConnect(t.Context(), conn)
	require.ErrorContains(t, err, `PostgreSQL schema "`+schema+`" is not accessible`)
}

func TestPoolConfigPreservesHooksAndLimits(t *testing.T) {
	maxConns := int32(8)
	minConns := int32(1)
	idleTime := time.Minute
	lifetime := 10 * time.Minute
	config, err := poolConfig(&PostgresConfig{
		Pool:            testPoolConfig(t, "postgres://user:password@database:5432/app?sslmode=disable"),
		VectorEnabled:   true,
		Schema:          "public",
		VectorSchema:    "public",
		MaxConns:        &maxConns,
		MinConns:        &minConns,
		MaxConnIdleTime: &idleTime,
		MaxConnLifetime: &lifetime,
	})
	require.NoError(t, err)

	assert.Nil(t, config.BeforeConnect)
	assert.NotNil(t, config.AfterConnect)
	assert.Equal(t, maxConns, config.MaxConns)
	assert.Equal(t, minConns, config.MinConns)
	assert.Equal(t, idleTime, config.MaxConnIdleTime)
	assert.Equal(t, lifetime, config.MaxConnLifetime)
}

func TestPoolConfigRequiresTableSchemaForVectors(t *testing.T) {
	_, err := poolConfig(&PostgresConfig{
		Pool:          testPoolConfig(t, "postgres://user:password@database:5432/app?sslmode=disable"),
		VectorEnabled: true,
		VectorSchema:  "public",
	})
	require.ErrorContains(t, err, "database schema is required")
}

func testPoolConfig(t *testing.T, url string) *pgxpool.Config {
	t.Helper()
	config, err := postgres.PoolConfig(url, "kagent_owner")
	require.NoError(t, err)
	return config
}
