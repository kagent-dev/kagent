// Package dbtest provides test helpers for spinning up a Postgres container.
package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	_ "github.com/lib/pq"
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

// Migrate runs the embedded OSS migrations against connStr and returns any error.
// If vectorEnabled is true the vector pass is also applied.
// Use MigrateT in tests that have a *testing.T; use Migrate in TestMain where no T is available.
func Migrate(connStr string, vectorEnabled bool) error {
	if err := runMigrationDir(connStr, "core", "schema_migrations"); err != nil {
		return fmt.Errorf("core migrations: %w", err)
	}
	if vectorEnabled {
		if err := runMigrationDir(connStr, "vector", "vector_schema_migrations"); err != nil {
			return fmt.Errorf("vector migrations: %w", err)
		}
	}
	return nil
}

// MigrateT runs the embedded OSS migrations against connStr and calls t.Fatal on error.
// If vectorEnabled is true the vector pass is also applied.
func MigrateT(t *testing.T, connStr string, vectorEnabled bool) {
	t.Helper()
	if err := Migrate(connStr, vectorEnabled); err != nil {
		t.Fatalf("dbtest.MigrateT: %v", err)
	}
}

// MigrateDown runs the embedded OSS down-migrations against connStr and returns any error.
// If vectorEnabled is true the vector pass is also rolled back first.
func MigrateDown(connStr string, vectorEnabled bool) error {
	if vectorEnabled {
		if err := downMigrationDir(connStr, "vector", "vector_schema_migrations"); err != nil {
			return fmt.Errorf("vector down migrations: %w", err)
		}
	}
	return downMigrationDir(connStr, "core", "schema_migrations")
}

func runMigrationDir(connStr, dir, migrationsTable string) error {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("open db for %s: %w", dir, err)
	}

	src, err := iofs.New(migrations.FS, dir)
	if err != nil {
		return fmt.Errorf("load migration files from %s: %w", dir, err)
	}

	driver, err := migratepg.WithInstance(db, &migratepg.Config{
		MigrationsTable: migrationsTable,
	})
	if err != nil {
		return fmt.Errorf("create migration driver for %s: %w", dir, err)
	}

	mg, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator for %s: %w", dir, err)
	}
	defer closeMigrate(dir, mg)

	if err := mg.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations for %s: %w", dir, err)
	}
	return nil
}

func downMigrationDir(connStr, dir, migrationsTable string) error {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("open db for %s: %w", dir, err)
	}

	src, err := iofs.New(migrations.FS, dir)
	if err != nil {
		return fmt.Errorf("load migration files from %s: %w", dir, err)
	}

	driver, err := migratepg.WithInstance(db, &migratepg.Config{
		MigrationsTable: migrationsTable,
	})
	if err != nil {
		return fmt.Errorf("create migration driver for %s: %w", dir, err)
	}

	mg, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator for %s: %w", dir, err)
	}
	defer closeMigrate(dir, mg)

	if err := mg.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("down migrations for %s: %w", dir, err)
	}
	return nil
}

// closeMigrate closes mg, logging source and database close errors separately.
func closeMigrate(dir string, mg *migrate.Migrate) {
	srcErr, dbErr := mg.Close()
	if srcErr != nil {
		fmt.Printf("warning: closing migration source for %s: %v\n", dir, srcErr)
	}
	if dbErr != nil {
		fmt.Printf("warning: closing migration database for %s: %v\n", dir, dbErr)
	}
}
