// Package dbtest provides test helpers for spinning up a Postgres container.
package dbtest

import (
	"context"
	"fmt"
	"testing"
	"time"

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
		return "", nil, fmt.Errorf("starting postgres container: %w", err)
	}

	connStr, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pgContainer.Terminate(ctx)
		return "", nil, fmt.Errorf("getting connection string: %w", err)
	}

	cleanup = func() {
		if err := pgContainer.Terminate(ctx); err != nil {
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
