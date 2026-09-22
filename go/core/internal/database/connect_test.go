package database

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestResolveURL(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		const url = "postgres://user:password@localhost:5432/database?sslmode=disable"
		got, err := ResolveURL(url)
		require.NoError(t, err)
		assert.Equal(t, url, got)
	})

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "connection-string")
		writeDatabaseURL(t, path, "  postgres://user:password@localhost:5432/database?sslmode=disable\n")

		got, err := ResolveURL("@file:" + path)
		require.NoError(t, err)
		assert.Equal(t, "postgres://user:password@localhost:5432/database?sslmode=disable", got)
	})

	t.Run("relative file", func(t *testing.T) {
		_, err := ResolveURL("@file:connection-string")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be absolute")
	})

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing")
		_, err := ResolveURL("@file:" + path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), path)
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "connection-string")
		writeDatabaseURL(t, path, " \n")
		_, err := ResolveURL("@file:" + path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is empty")
	})

	t.Run("malformed URL is redacted", func(t *testing.T) {
		_, err := ResolveURL("postgres://user:do-not-disclose@[invalid")
		require.Error(t, err)
		assert.ErrorIs(t, err, errInvalidDatabaseURL)
		assert.NotContains(t, err.Error(), "do-not-disclose")
	})
}

func TestPoolConfigRefreshesFileCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connection-string")
	const firstURL = "postgres://user:password-a@database:5432/app?sslmode=require&application_name=kagent"
	writeDatabaseURL(t, path, firstURL)

	config, err := poolConfig(&PostgresConfig{URL: "@file:" + path, Role: "kagent_app"})
	require.NoError(t, err)
	require.NotNil(t, config.BeforeConnect)
	require.NotNil(t, config.AfterConnect)
	assert.Equal(t, "password-a", config.ConnConfig.Password)

	connConfig := config.ConnConfig.Copy()
	initialTLS := connConfig.TLSConfig
	writeDatabaseURL(t, path, "postgres://user_v2:password-b@database:5432/app?sslmode=require&application_name=changed")
	require.NoError(t, config.BeforeConnect(context.Background(), connConfig))

	assert.Equal(t, "password-b", connConfig.Password)
	// The user rotates with the password; only the endpoint is fenced.
	assert.Equal(t, "user_v2", connConfig.User)
	assert.Equal(t, "user", config.ConnConfig.User, "refresh must not mutate the pinned config")
	assert.Equal(t, "kagent", connConfig.RuntimeParams["application_name"])
	assert.NotSame(t, initialTLS, connConfig.TLSConfig)
}

func TestPoolConfigRejectsRotatedUserWithoutStableRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connection-string")
	writeDatabaseURL(t, path, "postgres://user:password-a@database:5432/app?sslmode=disable")
	config, err := poolConfig(&PostgresConfig{URL: "@file:" + path})
	require.NoError(t, err)

	writeDatabaseURL(t, path, "postgres://user_v2:password-b@database:5432/app?sslmode=disable")
	err = config.BeforeConnect(context.Background(), config.ConnConfig.Copy())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a stable role")
}

func TestConnectRotatesLoginBehindStableRole(t *testing.T) {
	if testing.Short() {
		t.Skip("skip the PostgreSQL test in short mode")
	}
	const (
		role   = "kagent_rotation_role"
		loginA = "kagent_rotation_login_a"
		loginB = "kagent_rotation_login_b"
	)
	_, err := sharedDB.Exec(t.Context(), `
		DROP ROLE IF EXISTS kagent_rotation_login_a;
		DROP ROLE IF EXISTS kagent_rotation_login_b;
		DROP ROLE IF EXISTS kagent_rotation_role;
		CREATE ROLE kagent_rotation_role NOLOGIN;
		CREATE ROLE kagent_rotation_login_a LOGIN PASSWORD 'rotation-password';
		CREATE ROLE kagent_rotation_login_b LOGIN PASSWORD 'rotation-password';
		GRANT kagent_rotation_role TO kagent_rotation_login_a, kagent_rotation_login_b`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = sharedDB.Exec(context.Background(), `
			DROP ROLE IF EXISTS kagent_rotation_login_a;
			DROP ROLE IF EXISTS kagent_rotation_login_b;
			DROP ROLE IF EXISTS kagent_rotation_role`)
	})

	dsn, err := url.Parse(sharedConnStr)
	require.NoError(t, err)
	dsn.User = url.UserPassword(loginA, "rotation-password")
	path := filepath.Join(t.TempDir(), "connection-string")
	writeDatabaseURL(t, path, dsn.String())
	pool, err := Connect(t.Context(), &PostgresConfig{URL: "@file:" + path, Role: role})
	require.NoError(t, err)
	defer pool.Close()

	var sessionUser, currentUser string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT session_user, current_user`).Scan(&sessionUser, &currentUser))
	assert.Equal(t, loginA, sessionUser)
	assert.Equal(t, role, currentUser)

	dsn.User = url.UserPassword(loginB, "rotation-password")
	writeDatabaseURL(t, path, dsn.String())
	pool.Reset()
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT session_user, current_user`).Scan(&sessionUser, &currentUser))
	assert.Equal(t, loginB, sessionUser)
	assert.Equal(t, role, currentUser)
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

func TestPoolConfigRejectsRotatedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connection-string")
	const initial = "host=primary,secondary port=5432,5433 user=runtime password=password-a dbname=app sslmode=disable"

	tests := []struct {
		name string
		url  string
	}{
		{name: "host", url: "host=changed,secondary port=5432,5433 user=runtime password=password-b dbname=app sslmode=disable"},
		{name: "port", url: "host=primary,secondary port=6432,5433 user=runtime password=password-b dbname=app sslmode=disable"},
		{name: "database", url: "host=primary,secondary port=5432,5433 user=runtime password=password-b dbname=changed sslmode=disable"},
		{name: "fallback", url: "host=primary,changed port=5432,5433 user=runtime password=password-b dbname=app sslmode=disable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writeDatabaseURL(t, path, initial)
			config, err := poolConfig(&PostgresConfig{URL: "@file:" + path})
			require.NoError(t, err)

			writeDatabaseURL(t, path, test.url)
			err = config.BeforeConnect(context.Background(), config.ConnConfig.Copy())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "restart required")
			assert.NotContains(t, err.Error(), "password-a")
			assert.NotContains(t, err.Error(), "password-b")
		})
	}
}

func TestPoolConfigFailsSafelyWhenReplacementIsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connection-string")
	writeDatabaseURL(t, path, "postgres://user:password-a@database:5432/app?sslmode=disable")
	config, err := poolConfig(&PostgresConfig{URL: "@file:" + path})
	require.NoError(t, err)

	t.Run("malformed", func(t *testing.T) {
		writeDatabaseURL(t, path, "postgres://user:replacement-secret@[invalid")
		err := config.BeforeConnect(context.Background(), config.ConnConfig.Copy())
		require.Error(t, err)
		assert.ErrorIs(t, err, errInvalidDatabaseURL)
		assert.NotContains(t, err.Error(), "replacement-secret")
	})

	t.Run("empty", func(t *testing.T) {
		writeDatabaseURL(t, path, " \n")
		err := config.BeforeConnect(context.Background(), config.ConnConfig.Copy())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is empty")
	})

	t.Run("unreadable", func(t *testing.T) {
		require.NoError(t, os.Remove(path))
		err := config.BeforeConnect(context.Background(), config.ConnConfig.Copy())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read database connection source")
	})
}

func TestPoolConfigPreservesHooksAndLimits(t *testing.T) {
	maxConns := int32(8)
	minConns := int32(1)
	idleTime := time.Minute
	lifetime := 10 * time.Minute
	config, err := poolConfig(&PostgresConfig{
		URL:             "postgres://user:password@database:5432/app?sslmode=disable",
		VectorEnabled:   true,
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

func writeDatabaseURL(t *testing.T, path, url string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(url), 0o600))
}
