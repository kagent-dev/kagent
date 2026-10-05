// Package dbtest provides test helpers for spinning up a Postgres container.
package dbtest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Start starts a pgvector Postgres container and returns the connection string
// and a cleanup function. Callers are responsible for calling cleanup when done.
func Start(ctx context.Context) (connStr string, cleanup func(), err error) {
	pgContainer, err := tcpostgres.Run(ctx,
		"pgvector/pgvector:pg18-trixie",
		tcpostgres.WithDatabase("kagent_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("kagent"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		// Run returns the created container when it fails to start.
		return "", nil, errors.Join(fmt.Errorf("starting postgres container: %w", err), testcontainers.TerminateContainer(pgContainer))
	}

	connStr, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("getting connection string: %w", err), testcontainers.TerminateContainer(pgContainer))
	}

	cleanup = func() {
		// Not ctx: a test context is canceled before cleanup runs.
		if err := testcontainers.TerminateContainer(pgContainer); err != nil {
			fmt.Printf("warning: failed to terminate postgres container: %v\n", err)
		}
	}

	return connStr, cleanup, nil
}

// StartT starts a pgvector Postgres container and registers cleanup with t.Cleanup.
// Suitable for use in individual tests or test helpers that have a *testing.T.
func StartT(ctx context.Context, t *testing.T) string {
	t.Helper()

	connStr, cleanup, err := Start(ctx)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(cleanup)

	return connStr
}

// Migrate runs the embedded migrations against connStr and returns any error.
// If vectorEnabled is true the vector pass is also applied.
// Use MigrateT in tests that have a *testing.T; use Migrate in TestMain where no T is available.
func Migrate(connStr string, vectorEnabled bool) error {
	return migrations.RunUp(context.Background(), connStr, migrations.BuiltinSources(vectorEnabled))
}

// MigrateT runs the embedded migrations against connStr and calls t.Fatal on error.
// If vectorEnabled is true the vector pass is also applied.
func MigrateT(t *testing.T, connStr string, vectorEnabled bool) {
	t.Helper()
	if err := Migrate(connStr, vectorEnabled); err != nil {
		t.Fatalf("dbtest.MigrateT: %v", err)
	}
}
