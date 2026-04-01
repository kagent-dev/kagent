package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// RunUp applies all pending migrations for the given FS.
// vectorEnabled controls whether the vector track is also applied.
// Returns an error if any track fails (and attempts rollback of previously applied tracks).
func RunUp(url string, migrationsFS fs.FS, vectorEnabled bool) error {
	corePrev, err := applyDir(url, migrationsFS, "core", "schema_migrations")
	if err != nil {
		return fmt.Errorf("core migrations: %w", err)
	}

	if vectorEnabled {
		if _, err := applyDir(url, migrationsFS, "vector", "vector_schema_migrations"); err != nil {
			log.Printf("migrations: rolling back core to version %d", corePrev)
			rollbackDir(url, migrationsFS, "core", "schema_migrations", corePrev)
			return fmt.Errorf("vector migrations: %w", err)
		}
	}

	return nil
}

// RunDown rolls back steps migrations on a single track.
func RunDown(url string, migrationsFS fs.FS, dir, migrationsTable string, steps int) error {
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

// RunDownAll rolls back all applied migrations on a single track.
func RunDownAll(url string, migrationsFS fs.FS, dir, migrationsTable string) error {
	mg, err := newMigrate(url, migrationsFS, dir, migrationsTable)
	if err != nil {
		return err
	}
	defer closeMigrate(dir, mg)

	if err := mg.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("down migrations for %s: %w", dir, err)
	}
	return nil
}

// RunVersion returns the current applied version and dirty flag for a single track.
func RunVersion(url string, migrationsFS fs.FS, dir, migrationsTable string) (version uint, dirty bool, err error) {
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

// RunForce forces the tracking table for a single track to version (clears the dirty flag).
// Pass version=-1 to remove the version record entirely.
func RunForce(url string, migrationsFS fs.FS, dir, migrationsTable string, version int) error {
	mg, err := newMigrate(url, migrationsFS, dir, migrationsTable)
	if err != nil {
		return err
	}
	defer closeMigrate(dir, mg)

	if err := mg.Force(version); err != nil {
		return fmt.Errorf("force %s to version %d: %w", dir, version, err)
	}
	return nil
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
		log.Printf("migrations: migration failed for %s, attempting rollback to version %d", dir, prevVersion)
		if rbErr := rollbackToVersion(mg, dir, prevVersion); rbErr != nil {
			log.Printf("migrations: rollback failed for %s: %v", dir, rbErr)
		} else {
			log.Printf("migrations: rolled back %s to version %d", dir, prevVersion)
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
		log.Printf("migrations: rollback of %s failed (open): %v", dir, err)
		return
	}
	defer closeMigrate(dir, mg)
	if err := rollbackToVersion(mg, dir, targetVersion); err != nil {
		log.Printf("migrations: rollback of %s failed: %v", dir, err)
	} else {
		log.Printf("migrations: rolled back %s to version %d", dir, targetVersion)
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

// newMigrate opens a dedicated database connection and constructs a migrate.Migrate
// for the given dir/table. The caller must call closeMigrate when done.
// Uses sql.Open (pgx stdlib shim) — a single dedicated connection — not a pool,
// because the advisory lock is session-level and must not be shared.
func newMigrate(url string, migrationsFS fs.FS, dir, migrationsTable string) (*migrate.Migrate, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open database for %s: %w", dir, err)
	}

	src, err := iofs.New(migrationsFS, dir)
	if err != nil {
		return nil, fmt.Errorf("load migration files from %s: %w", dir, err)
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{
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
