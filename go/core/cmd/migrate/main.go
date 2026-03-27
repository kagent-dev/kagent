// kagent-migrate runs Postgres schema migrations and exits.
// It is intended to run as a Kubernetes init container before the kagent
// controller starts, ensuring the schema is up to date before the app connects.
//
// Usage:
//
//	kagent-migrate [command]
//
// Commands:
//
//	up       Apply all pending migrations (default when no command is given)
//	down     Roll back N migrations on a single track
//	version  Print the current applied version and dirty flag for each track
//
// Required environment variable:
//
//	POSTGRES_DATABASE_URL  — Postgres connection URL
//
// Optional environment variables:
//
//	POSTGRES_DATABASE_URL_FILE     — path to a file containing the URL (takes precedence)
//	KAGENT_DATABASE_VECTOR_ENABLED — set to "true" to also run vector migrations
//
// Enterprise extension: replace this binary with enterprise-migrate, which imports
// go/core/pkg/migrations.FS directly via the OSS Go module dependency and adds its
// own migration passes alongside it at compile time.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	_ "github.com/lib/pq"
)

func main() {
	flag.Parse()

	url, err := resolveURL()
	if err != nil {
		log.Fatalf("kagent-migrate: %v", err)
	}

	vectorEnabled := strings.EqualFold(os.Getenv("KAGENT_DATABASE_VECTOR_ENABLED"), "true")

	cmd := "up"
	args := flag.Args()
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "up":
		runUpCommand(url, migrations.FS, vectorEnabled)
	case "down":
		runDownCommand(url, migrations.FS, vectorEnabled, args)
	case "version":
		runVersionCommand(url, migrations.FS, vectorEnabled)
	default:
		log.Fatalf("kagent-migrate: unknown command %q (valid: up, down, version)", cmd)
	}
}

func runUpCommand(url string, migrationsFS fs.FS, vectorEnabled bool) {
	corePrev, err := applyDir(url, migrationsFS, "core", "schema_migrations")
	if err != nil {
		log.Fatalf("kagent-migrate: core migrations: %v", err)
	}
	log.Println("kagent-migrate: core migrations applied")

	if vectorEnabled {
		if _, err := applyDir(url, migrationsFS, "vector", "vector_schema_migrations"); err != nil {
			// Vector failed (and already rolled itself back). Roll back core too
			// since both tracks are treated as one unit.
			log.Printf("kagent-migrate: rolling back core to version %d", corePrev)
			rollbackDir(url, migrationsFS, "core", "schema_migrations", corePrev)
			log.Fatalf("kagent-migrate: vector migrations: %v", err)
		}
		log.Println("kagent-migrate: vector migrations applied")
	}

	log.Println("kagent-migrate: done")
}

// applyDir runs Up for dir and rolls back on failure. It returns the pre-run
// version so the caller can roll back this track if a later track fails.
func applyDir(url string, migrationsFS fs.FS, dir, migrationsTable string) (prevVersion uint, err error) {
	mg, err := newMigrate(url, migrationsFS, dir, migrationsTable)
	if err != nil {
		return 0, err
	}
	defer closeMigrate(dir, mg)

	prevVersion, _, err = mg.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, fmt.Errorf("get pre-migration version for %s: %w", dir, err)
	}
	// prevVersion == 0 when ErrNilVersion (no migrations applied yet).

	if upErr := mg.Up(); upErr != nil {
		if errors.Is(upErr, migrate.ErrNoChange) {
			return prevVersion, nil
		}
		log.Printf("kagent-migrate: migration failed for %s, attempting rollback to version %d", dir, prevVersion)
		if rbErr := rollbackToVersion(mg, dir, prevVersion); rbErr != nil {
			log.Printf("kagent-migrate: rollback failed for %s: %v", dir, rbErr)
		} else {
			log.Printf("kagent-migrate: rolled back %s to version %d", dir, prevVersion)
		}
		return prevVersion, fmt.Errorf("run migrations for %s: %w", dir, upErr)
	}
	return prevVersion, nil
}

// rollbackDir opens a fresh migrate instance and rolls dir back to targetVersion.
// Used to roll back a previously-succeeded track when a later track fails.
func rollbackDir(url string, migrationsFS fs.FS, dir, migrationsTable string, targetVersion uint) {
	mg, err := newMigrate(url, migrationsFS, dir, migrationsTable)
	if err != nil {
		log.Printf("kagent-migrate: rollback of %s failed (open): %v", dir, err)
		return
	}
	defer closeMigrate(dir, mg)
	if err := rollbackToVersion(mg, dir, targetVersion); err != nil {
		log.Printf("kagent-migrate: rollback of %s failed: %v", dir, err)
	} else {
		log.Printf("kagent-migrate: rolled back %s to version %d", dir, targetVersion)
	}
}

// rollbackToVersion rolls the migration state back to targetVersion.
// It handles the dirty-state cleanup golang-migrate requires after a failed
// Up run before down steps can be applied.
func rollbackToVersion(mg *migrate.Migrate, dir string, targetVersion uint) error {
	currentVersion, dirty, err := mg.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return nil // nothing was applied; nothing to roll back
		}
		return fmt.Errorf("get version after failure for %s: %w", dir, err)
	}

	if dirty {
		// The failed migration is recorded as dirty at currentVersion.
		// Force to the last clean version so Steps can run.
		cleanVersion := int(currentVersion) - 1
		forceTarget := cleanVersion
		if forceTarget < 1 {
			forceTarget = -1 // negative tells golang-migrate to remove the version record entirely
		}
		if err := mg.Force(forceTarget); err != nil {
			return fmt.Errorf("clear dirty state for %s: %w", dir, err)
		}
		if forceTarget < 0 {
			return nil // first migration failed and was cleared; nothing left to roll back
		}
		currentVersion = uint(cleanVersion)
	}

	steps := int(currentVersion) - int(targetVersion)
	if steps <= 0 {
		return nil
	}
	if err := mg.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back %d step(s) for %s: %w", steps, dir, err)
	}
	return nil
}

func runDownCommand(url string, migrationsFS fs.FS, vectorEnabled bool, args []string) {
	downFlags := flag.NewFlagSet("down", flag.ExitOnError)
	steps := downFlags.Int("steps", 0, "number of down migrations to run (required, must be > 0)")
	track := downFlags.String("track", "core", "migration track to roll back: core or vector")
	if err := downFlags.Parse(args); err != nil {
		log.Fatalf("kagent-migrate: down: %v", err)
	}

	if *steps <= 0 {
		log.Fatalf("kagent-migrate: down: --steps must be a positive integer")
	}

	var dir, table string
	switch *track {
	case "core":
		dir, table = "core", "schema_migrations"
	case "vector":
		if !vectorEnabled {
			log.Fatalf("kagent-migrate: down: track %q requested but KAGENT_DATABASE_VECTOR_ENABLED is not true", *track)
		}
		dir, table = "vector", "vector_schema_migrations"
	default:
		log.Fatalf("kagent-migrate: down: unknown track %q (valid: core, vector)", *track)
	}

	if err := downDir(url, migrationsFS, dir, table, *steps); err != nil {
		log.Fatalf("kagent-migrate: down %s (%d steps): %v", *track, *steps, err)
	}
	log.Printf("kagent-migrate: rolled back %d migration(s) on %s track", *steps, *track)
}

func runVersionCommand(url string, migrationsFS fs.FS, vectorEnabled bool) {
	tracks := []struct{ dir, table string }{
		{"core", "schema_migrations"},
	}
	if vectorEnabled {
		tracks = append(tracks, struct{ dir, table string }{"vector", "vector_schema_migrations"})
	}

	for _, t := range tracks {
		version, dirty, err := versionDir(url, migrationsFS, t.dir, t.table)
		if err != nil {
			log.Fatalf("kagent-migrate: version %s: %v", t.dir, err)
		}
		log.Printf("kagent-migrate: track=%-6s table=%-30s version=%d dirty=%v", t.dir, t.table, version, dirty)
	}
}

func resolveURL() (string, error) {
	if file := os.Getenv("POSTGRES_DATABASE_URL_FILE"); file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading URL file %s: %w", file, err)
		}
		url := strings.TrimSpace(string(content))
		if url == "" {
			return "", fmt.Errorf("URL file %s is empty", file)
		}
		return url, nil
	}
	url := os.Getenv("POSTGRES_DATABASE_URL")
	if url == "" {
		return "", fmt.Errorf("POSTGRES_DATABASE_URL must be set")
	}
	return url, nil
}

// newMigrate opens a database connection and constructs a migrate.Migrate for the given dir/table.
// The caller is responsible for calling closeMigrate on the returned instance.
func newMigrate(url string, migrationsFS fs.FS, dir, migrationsTable string) (*migrate.Migrate, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open database for %s: %w", dir, err)
	}

	src, err := iofs.New(migrationsFS, dir)
	if err != nil {
		return nil, fmt.Errorf("load migration files from %s: %w", dir, err)
	}

	driver, err := migratepg.WithInstance(db, &migratepg.Config{
		MigrationsTable: migrationsTable,
	})
	if err != nil {
		return nil, fmt.Errorf("create migration driver for %s: %w", dir, err)
	}

	mg, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return nil, fmt.Errorf("create migrator for %s: %w", dir, err)
	}
	return mg, nil
}

func downDir(url string, migrationsFS fs.FS, dir, migrationsTable string, steps int) error {
	mg, err := newMigrate(url, migrationsFS, dir, migrationsTable)
	if err != nil {
		return err
	}
	defer closeMigrate(dir, mg)

	if err := mg.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back %d migration(s) for %s: %w", steps, dir, err)
	}
	return nil
}

func versionDir(url string, migrationsFS fs.FS, dir, migrationsTable string) (version uint, dirty bool, err error) {
	mg, err := newMigrate(url, migrationsFS, dir, migrationsTable)
	if err != nil {
		return 0, false, err
	}
	defer closeMigrate(dir, mg)

	version, dirty, err = mg.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, fmt.Errorf("get version for %s: %w", dir, err)
	}
	return version, dirty, nil
}

// closeMigrate closes mg, logging source and database close errors separately.
func closeMigrate(dir string, mg *migrate.Migrate) {
	srcErr, dbErr := mg.Close()
	if srcErr != nil {
		log.Printf("warning: closing migration source for %s: %v", dir, srcErr)
	}
	if dbErr != nil {
		log.Printf("warning: closing migration database for %s: %v", dir, dbErr)
	}
}
