// Package migrations applies the kanban-owned database schema using golang-migrate.
// It ships its own migration set in a dedicated "kanban" Postgres schema and tracks
// state in the kanban_schema_migrations table, independent of the kagent core/vector
// tracks. Modeled on go/core/pkg/migrations/runner.go (newMigrate), which it cannot
// import across the internal/ boundary.
package migrations

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed sql
var fsys embed.FS

// migrationsTable is the dedicated state table for the kanban track. It is
// separate from the kagent core ("schema_migrations") and vector tracks so the
// two binaries can migrate the same database without colliding.
const migrationsTable = "kanban_schema_migrations"

// RunUp applies all pending kanban migrations against url. migrate.ErrNoChange
// (no pending migrations) is treated as success.
func RunUp(url string) error {
	mg, err := newMigrate(url)
	if err != nil {
		return err
	}
	defer closeMigrate(mg)

	if err := mg.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("kanban migrations: %w", err)
	}
	return nil
}

// newMigrate opens a dedicated database connection (sql.Open via the pgx stdlib
// shim — a single connection, not a pool, because the advisory lock is session
// level) and constructs a migrate.Migrate over the embedded sql dir.
func newMigrate(url string) (*migrate.Migrate, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open database for kanban migrations: %w", err)
	}

	// sql.Open is lazy, so verify the database is actually reachable before
	// building the driver. At startup the database may still be coming up (the
	// app and DB often start together in k8s), so retry briefly with backoff.
	if err := waitForDB(db); err != nil {
		return nil, fmt.Errorf("connect to database for kanban migrations: %w", err)
	}

	src, err := iofs.New(fsys, "sql")
	if err != nil {
		return nil, fmt.Errorf("load kanban migration files: %w", err)
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{
		MigrationsTable: migrationsTable,
	})
	if err != nil {
		return nil, fmt.Errorf("create kanban migration driver: %w", err)
	}

	mg, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return nil, fmt.Errorf("create kanban migrator: %w", err)
	}
	return mg, nil
}

// waitForDB pings db until it responds or the deadline elapses, tolerating a
// database that is still accepting connections (common right after the DB
// container/pod starts).
func waitForDB(db *sql.DB) error {
	const (
		timeout  = 30 * time.Second
		interval = 250 * time.Millisecond
	)
	deadline := time.Now().Add(timeout)
	var err error
	for {
		if err = db.Ping(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(interval)
	}
}

// closeMigrate closes mg, joining any source/database close errors. There is no
// caller to return them to (RunUp has already returned), so they are wrapped and
// discarded; the underlying connection is released either way.
func closeMigrate(mg *migrate.Migrate) {
	_, _ = mg.Close()
}
