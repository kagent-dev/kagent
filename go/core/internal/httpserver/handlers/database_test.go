package handlers_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	apidatabase "github.com/kagent-dev/kagent/go/api/database"
	coredatabase "github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/stretchr/testify/require"
)

var (
	sharedDB        *pgxpool.Pool
	sharedDBCleanup func()
	sharedDBInitErr error
	sharedDBInit    sync.Once
)

func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if sharedDB != nil {
		sharedDB.Close()
	}
	if sharedDBCleanup != nil {
		sharedDBCleanup()
	}
	os.Exit(code)
}

func setupTestDBClient(t *testing.T) apidatabase.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database-backed handler test in short mode")
	}

	initSharedDB(t)

	tableNames, err := truncatableTables(context.Background())
	require.NoError(t, err, "failed to list tables for truncation")

	_, err = sharedDB.Exec(context.Background(), fmt.Sprintf(
		"TRUNCATE TABLE %s RESTART IDENTITY CASCADE",
		strings.Join(tableNames, ", "),
	))
	require.NoError(t, err, "failed to truncate test tables")

	return coredatabase.NewClient(sharedDB)
}

func initSharedDB(t *testing.T) {
	t.Helper()

	sharedDBInit.Do(func() {
		connStr, cleanup, err := dbtest.Start(context.Background())
		if err != nil {
			sharedDBInitErr = fmt.Errorf("start postgres container: %w", err)
			return
		}

		if err := dbtest.Migrate(connStr, true); err != nil {
			cleanup()
			sharedDBInitErr = fmt.Errorf("migrate test database: %w", err)
			return
		}

		db, err := coredatabase.Connect(context.Background(), &coredatabase.PostgresConfig{
			URL:           connStr,
			VectorEnabled: true,
		})
		if err != nil {
			cleanup()
			sharedDBInitErr = fmt.Errorf("connect to test database: %w", err)
			return
		}

		sharedDB = db
		sharedDBCleanup = cleanup
	})

	require.NoError(t, sharedDBInitErr, "failed to initialize shared test database")
}

func truncatableTables(ctx context.Context) ([]string, error) {
	rows, err := sharedDB.Query(ctx, `
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = current_schema()
		  AND tablename NOT IN ('schema_migrations', 'vector_schema_migrations')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, quoteIdentifier(tableName))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	slices.Sort(tableNames)
	return tableNames, nil
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
