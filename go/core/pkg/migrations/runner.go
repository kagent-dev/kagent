package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("migrations")

// RunUp applies all pending migrations for the given FS.
// vectorEnabled controls whether the vector track is also applied.
// Returns an error if any track fails (and attempts rollback of previously applied tracks).
func RunUp(url string, migrationsFS fs.FS, vectorEnabled, allowInitialMigrationRollback bool) error {
	if vectorEnabled {
		if err := checkPgvector(url); err != nil {
			return fmt.Errorf("vector migrations require pgvector: %w", err)
		}
	}

	corePrev, err := applyDir(url, migrationsFS, "core", "schema_migrations", allowInitialMigrationRollback)
	if err != nil {
		return fmt.Errorf("core migrations: %w", err)
	}

	if vectorEnabled {
		if _, err := applyDir(url, migrationsFS, "vector", "vector_schema_migrations", allowInitialMigrationRollback); err != nil {
			if corePrev > 0 || allowInitialMigrationRollback {
				log.Info("rolling back core after vector failure", "targetVersion", corePrev)
				rollbackDir(url, migrationsFS, "core", "schema_migrations", corePrev)
			} else {
				log.Info("vector migration failed; skipping core rollback because no prior core version exists (set --database-allow-initial-migration-rollback to override)", "corePrev", corePrev)
			}
			return fmt.Errorf("vector migrations: %w", err)
		}
	}

	return nil
}

// applyDir runs Up for dir and rolls back on failure. If no migrations have
// ever completed successfully (prevVersion == 0), rollback is skipped to avoid
// dropping pre-existing tables on a GORM-to-golang-migrate upgrade.
// It returns the pre-run version so the caller can roll back this track if a later track fails.
func applyDir(url string, migrationsFS fs.FS, dir, migrationsTable string, allowInitialMigrationRollback bool) (prevVersion uint, err error) {
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
		if prevVersion > 0 || allowInitialMigrationRollback {
			log.Info("migration failed, attempting rollback", "track", dir, "targetVersion", prevVersion, "allowInitialMigrationRollback", allowInitialMigrationRollback)
			if rbErr := rollbackToVersion(mg, dir, prevVersion); rbErr != nil {
				log.Error(rbErr, "rollback failed", "track", dir)
			} else {
				log.Info("rollback complete", "track", dir, "version", prevVersion)
			}
		} else {
			log.Info("migration failed; skipping rollback because no prior version exists (set --database-allow-initial-migration-rollback to override)", "track", dir)
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
		log.Error(err, "rollback failed (open)", "track", dir)
		return
	}
	defer closeMigrate(dir, mg)
	if err := rollbackToVersion(mg, dir, targetVersion); err != nil {
		log.Error(err, "rollback failed", "track", dir)
	} else {
		log.Info("rollback complete", "track", dir, "version", targetVersion)
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

// checkPgvector verifies that the pgvector extension is available on the database.
// This is called before running vector migrations to fail fast with a clear error
// rather than failing mid-migration and triggering a rollback.
func checkPgvector(url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	var available bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name = 'vector')").Scan(&available)
	if err != nil {
		return fmt.Errorf("check pgvector availability: %w", err)
	}
	if !available {
		return fmt.Errorf("the pgvector extension is not installed on this PostgreSQL instance; either install pgvector or set --database-vector-enabled=false")
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
		log.Error(srcErr, "closing migration source", "track", dir)
	}
	if dbErr != nil {
		log.Error(dbErr, "closing migration database", "track", dir)
	}
}
