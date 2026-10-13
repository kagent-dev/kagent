package database

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryDBConnection_DeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := retryDBConnection(ctx, &PostgresConfig{
		URL: "postgres://user:pass@localhost:1/nodb?connect_timeout=1",
	})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
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

func TestValidateURL(t *testing.T) {
	const valid = "postgres://user:password@localhost:5432/database?sslmode=disable"
	require.NoError(t, ValidateURL(valid))

	err := ValidateURL("postgres://user:do-not-disclose@[invalid")
	require.Error(t, err)
	assert.ErrorIs(t, err, errInvalidDatabaseURL)
	assert.NotContains(t, err.Error(), "do-not-disclose")
}

func TestPoolConfigSetsSchema(t *testing.T) {
	config, err := poolConfig(&PostgresConfig{
		URL:    "postgres://user:password@database:5432/app?sslmode=disable",
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
	config, err := poolConfig(&PostgresConfig{URL: sharedConnStr, Schema: schema})
	require.NoError(t, err)
	conn, err := pgx.ConnectConfig(t.Context(), config.ConnConfig.Copy())
	require.NoError(t, err)
	defer conn.Close(t.Context())

	err = config.AfterConnect(t.Context(), conn)
	require.ErrorContains(t, err, `PostgreSQL schema "`+schema+`" is not accessible`)
}

func TestPoolConfigRefreshesTLSForLiteralURL(t *testing.T) {
	config, err := poolConfig(&PostgresConfig{
		URL: "postgres://user:static-password@database:5432/app?sslmode=require",
	})
	require.NoError(t, err)
	require.NotNil(t, config.BeforeConnect)

	connConfig := config.ConnConfig.Copy()
	initialTLS := connConfig.TLSConfig
	connConfig.Password = "unchanged-password"
	require.NoError(t, config.BeforeConnect(context.Background(), connConfig))

	assert.Equal(t, "unchanged-password", connConfig.Password)
	assert.NotSame(t, initialTLS, connConfig.TLSConfig)
}

func TestPoolConfigPreservesHooksAndLimits(t *testing.T) {
	maxConns := int32(8)
	minConns := int32(1)
	idleTime := time.Minute
	lifetime := 10 * time.Minute
	config, err := poolConfig(&PostgresConfig{
		URL:             "postgres://user:password@database:5432/app?sslmode=disable",
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
		URL:           "postgres://user:password@database:5432/app?sslmode=disable",
		VectorEnabled: true,
		VectorSchema:  "public",
	})
	require.ErrorContains(t, err, "database schema is required")
}
