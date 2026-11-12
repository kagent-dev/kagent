package dbtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

func TestClonedDatabasesAreIsolatedAndRemoved(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL test in short mode")
	}
	ctx := context.Background()
	templateDSN := dbtest.StartT(ctx, t)
	template, err := pgx.Connect(ctx, templateDSN)
	require.NoError(t, err)
	_, err = template.Exec(ctx, "CREATE TABLE fixture (id integer PRIMARY KEY, value text NOT NULL); INSERT INTO fixture VALUES (1, 'seed')")
	require.NoError(t, err)
	require.NoError(t, template.Close(ctx))

	var databaseNames []string
	t.Run("Given a seeded template, when two clones write the same key, assert that their data stays independent", func(t *testing.T) {
		var connections []*pgx.Conn
		values := []string{"first", "second"}
		for _, value := range values {
			dsn := dbtest.CloneT(t, templateDSN)
			conn, err := pgx.Connect(t.Context(), dsn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close(ctx)) })
			connections = append(connections, conn)

			var name string
			require.NoError(t, conn.QueryRow(t.Context(), "SELECT current_database()").Scan(&name))
			databaseNames = append(databaseNames, name)
			_, err = conn.Exec(t.Context(), "INSERT INTO fixture VALUES (2, $1)", value)
			require.NoError(t, err)
		}
		for i, conn := range connections {
			var seed, value string
			require.NoError(t, conn.QueryRow(t.Context(), "SELECT value FROM fixture WHERE id = 1").Scan(&seed))
			require.Equal(t, "seed", seed)
			require.NoError(t, conn.QueryRow(t.Context(), "SELECT value FROM fixture WHERE id = 2").Scan(&value))
			require.Equal(t, values[i], value)
		}
	})

	template, err = pgx.Connect(ctx, templateDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, template.Close(ctx)) })
	var count int
	require.NoError(t, template.QueryRow(ctx, "SELECT count(*) FROM fixture").Scan(&count))
	require.Equal(t, 1, count, "clones must not modify the template")
	require.NoError(t, template.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = ANY($1)", databaseNames).Scan(&count))
	require.Zero(t, count, "test cleanup must remove every clone")
}
