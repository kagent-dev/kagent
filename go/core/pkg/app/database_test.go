package app

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
)

func TestDatabaseConfigFromEnv(t *testing.T) {
	for _, name := range []string{kagentenv.DBMaxConns.Name(), kagentenv.DBMinConns.Name(), kagentenv.DBMaxConnIdleTime.Name(), kagentenv.DBMaxConnLifetime.Name()} {
		t.Setenv(name, "")
	}
	config, err := databaseConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, database.PostgresConfig{}, config, "unset options must preserve connection URL and pgx defaults")

	for _, test := range []struct {
		variable string
		input    string
	}{
		{kagentenv.DBMaxConns.Name(), "many"},
		{kagentenv.DBMaxConns.Name(), "0"},
		{kagentenv.DBMinConns.Name(), "-1"},
		{kagentenv.DBMaxConns.Name(), "2147483648"},
		{kagentenv.DBMaxConnIdleTime.Name(), "soon"},
		{kagentenv.DBMaxConnIdleTime.Name(), "0s"},
		{kagentenv.DBMaxConnLifetime.Name(), "-1m"},
	} {
		t.Run(test.variable+"/"+test.input, func(t *testing.T) {
			t.Setenv(test.variable, test.input)
			_, err := databaseConfigFromEnv()
			require.ErrorContains(t, err, test.variable)
		})
	}
	t.Setenv(kagentenv.DBMaxConns.Name(), "8")
	t.Setenv(kagentenv.DBMinConns.Name(), "0")
	t.Setenv(kagentenv.DBMaxConnIdleTime.Name(), "30s")
	t.Setenv(kagentenv.DBMaxConnLifetime.Name(), "10m")
	config, err = databaseConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, database.PostgresConfig{
		MaxConns: new(int32(8)), MinConns: new(int32(0)),
		MaxConnIdleTime: new(30 * time.Second), MaxConnLifetime: new(10 * time.Minute),
	}, config)
}

func TestDatabasePoolClosesIdleConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(cancel)
	conn, cleanup, err := dbtest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	u, err := url.Parse(conn)
	require.NoError(t, err)
	query := u.Query()
	query.Set("pool_health_check_period", "10ms")
	query.Set("pool_min_conns", "2") // Environment settings override the URL.
	query.Set("pool_max_conn_idle_time", "1h")
	u.RawQuery = query.Encode()
	t.Setenv(kagentenv.DBMaxConns.Name(), "4")
	t.Setenv(kagentenv.DBMinConns.Name(), "0")
	t.Setenv(kagentenv.DBMaxConnIdleTime.Name(), "50ms")
	t.Setenv(kagentenv.DBMaxConnLifetime.Name(), "1h")
	config, err := databaseConfigFromEnv()
	require.NoError(t, err)
	config.URL = u.String()
	pool, err := database.Connect(t.Context(), &config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.Equal(t, int32(0), pool.Config().MinConns)
	require.Equal(t, 50*time.Millisecond, pool.Config().MaxConnIdleTime)
	require.Eventually(t, func() bool { return pool.Stat().TotalConns() == 0 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, pool.Ping(t.Context()), "the next request must reopen a connection")
}
