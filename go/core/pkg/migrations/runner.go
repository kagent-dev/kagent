package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	nurl "net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("migrations")

// Source describes one migration track for the orchestrator to apply. Downstream
// consumers register their own Sources alongside the built-in ones rather than
// owning a runner, so track ordering and failure handling stay centralized.
type Source struct {
	// Name labels the source in logs and errors (e.g. "core", "vector").
	Name string
	// Schema is the Postgres schema the track lives in. Empty means the
	// connection's default schema (resolved via search_path / current_schema),
	// which is what the built-in tracks use. A non-empty value scopes the
	// tracking table and migration objects to that schema: the orchestrator
	// creates it (CREATE SCHEMA IF NOT EXISTS) and sets search_path on the
	// connection. Schema is treated as an untrusted identifier and validated.
	//
	// When Schema is empty (the built-in tracks), the DSN is left untouched:
	// the connection keeps the server's default search_path ("$user", public), so
	// migration objects and the tracking table land in public exactly as before
	// this orchestrator existed. The schema handling below applies only when
	// Schema is set.
	//
	// When Schema is set, search_path is pinned to it alone (pg_catalog is always
	// implicitly searched, so built-in types and functions still resolve). public
	// is NOT on the path, which keeps a schema-scoped track strictly isolated. A
	// migration that needs a shared extension installed in public (e.g. the
	// pgvector "vector" type) must therefore either install/relocate the extension
	// into this schema or schema-qualify the reference; it cannot rely on public.
	//
	// Do not register two sources whose schemas resolve to the same value with the
	// same TrackingTable — e.g. one source with Schema == "" and another naming the
	// connection's current_schema() explicitly. They would share one tracking-table
	// row and one advisory lock and corrupt each other's state. The collision unit
	// is (resolved schema, TrackingTable). validateSources catches collisions on the
	// literal Schema; RunUp additionally resolves "" to current_schema() and rejects
	// collisions that only appear after resolution.
	Schema string
	// TrackingTable is the golang-migrate bookkeeping table for this track.
	TrackingTable string
	// FS holds the embedded migration files.
	FS fs.FS
	// Dir is the subdirectory within FS that holds this track's files.
	Dir string
	// PreCheck, if set, runs before any source is applied. A non-nil error
	// aborts the whole run before any migration executes (fail-fast).
	PreCheck func(url string) error
}

// BuiltinSources returns the built-in source set: the core track always, and the
// vector track when vectorEnabled. app.Start prepends these to any
// downstream-registered extra sources before calling RunUp, so the built-in
// tracks always run first and downstream consumers only supply their own extras
// (never assembling this slice themselves). A caller that invokes RunUp directly
// (e.g. a migration CLI) composes the list the same way: BuiltinSources first,
// then extras.
func BuiltinSources(vectorEnabled bool) []Source {
	sources := []Source{{
		Name:          "core",
		TrackingTable: "schema_migrations",
		FS:            FS,
		Dir:           "core",
	}}
	if vectorEnabled {
		sources = append(sources, Source{
			Name:          "vector",
			TrackingTable: "vector_schema_migrations",
			FS:            FS,
			Dir:           "vector",
			PreCheck:      checkPgvector,
		})
	}
	return sources
}

// RunUp applies all pending migrations for each source, in slice order.
//
// All PreChecks run first, before any source is applied, so a failed precheck
// aborts the run before touching the database. Each source is then applied with
// the same per-track safety behavior: it tolerates a database ahead of this
// binary (compatibility mode), refuses a dirty-and-ahead database, and rolls
// itself back if its own Up fails. If a later source fails, previously-applied
// sources are rolled back to their pre-run versions in reverse order.
//
// ctx is honored at source boundaries and during schema setup and prechecks.
// golang-migrate's apply is not context-aware, so an in-flight migration is not
// cancellable.
func RunUp(ctx context.Context, url string, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	if err := validateSources(sources); err != nil {
		return err
	}
	// Catch collisions that only appear once "" schemas resolve to current_schema().
	if err := checkResolvedSchemaCollisions(ctx, url, sources); err != nil {
		return err
	}

	// Run every precheck up front so a failure aborts before any source applies
	// (e.g. pgvector is verified before the core track runs).
	for _, src := range sources {
		if src.PreCheck == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("migrations cancelled before %s precheck: %w", src.Name, err)
		}
		if err := src.PreCheck(url); err != nil {
			return fmt.Errorf("%s precheck: %w", src.Name, err)
		}
	}

	type applied struct {
		src  Source
		prev uint
	}
	var done []applied

	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("migrations cancelled before %s: %w", src.Name, err)
		}
		prev, err := applySource(ctx, url, src)
		if err != nil {
			// Compensating rollback: undo previously-applied sources in reverse
			// order, each to its own pre-run version. The failing source has
			// already rolled itself back in applySource.
			var compErrs []error
			for _, a := range slices.Backward(done) {
				if a.prev == 0 {
					log.Info("skipping compensating rollback to version 0 to protect pre-existing data", "source", a.src.Name)
					continue
				}
				log.Info("rolling back source after later failure", "source", a.src.Name, "targetVersion", a.prev)
				if rbErr := rollbackSource(ctx, url, a.src, a.prev); rbErr != nil {
					compErrs = append(compErrs, rbErr)
				}
			}
			runErr := fmt.Errorf("%s migrations: %w", src.Name, err)
			if len(compErrs) > 0 {
				// A compensating rollback failed: the database may be left in a
				// partially rolled-back state. Surface it alongside the original
				// failure rather than only in the logs.
				return errors.Join(append([]error{runErr}, compErrs...)...)
			}
			return runErr
		}
		done = append(done, applied{src: src, prev: prev})
	}

	return nil
}

// validateSources rejects two sources that share the same (schema, tracking
// table). That pair is the collision unit: it is what determines the
// golang-migrate version row and the advisory-lock id, so two such sources would
// fight over one row and lock regardless of their Dir/FS. Distinct tracking
// tables in the same schema are fine (each gets its own bookkeeping).
//
// This keys on the *literal* Schema string and is intentionally DB-free so it
// stays unit-testable. It therefore cannot see a collision between a source with
// Schema == "" and one naming the connection's current_schema() explicitly, since
// "" resolves to that schema only at connection time. checkResolvedSchemaCollisions
// (called from RunUp, where a connection is available) closes that gap.
func validateSources(sources []Source) error {
	seen := make(map[string]string, len(sources))
	for _, s := range sources {
		// Validate schema names up front so a bad name on a later source aborts
		// the whole run before any earlier source applies, matching the fail-fast
		// guarantee RunUp gives prechecks. newMigrate re-validates as a safety net
		// for callers that bypass RunUp (e.g. applySource directly in tests).
		if s.Schema != "" {
			if err := validateSchemaName(s.Schema); err != nil {
				return err
			}
		}
		key := s.Schema + "\x00" + s.TrackingTable
		if other, ok := seen[key]; ok {
			return fmt.Errorf("sources %q and %q share tracking table %q in schema %q", other, s.Name, s.TrackingTable, s.Schema)
		}
		seen[key] = s.Name
	}
	return nil
}

// checkResolvedSchemaCollisions rejects sources that collide only after their
// schemas are resolved — a source with Schema == "" and another naming the
// connection's current_schema() explicitly both land in the same schema, so with
// the same TrackingTable they would share one golang-migrate version row and one
// advisory lock. validateSources cannot see this because it keys on the literal
// Schema; resolving "" requires a connection, which is why this lives here.
//
// The query runs only when the source set mixes empty and explicit schemas;
// all-empty (the built-in core+vector default) and all-explicit sets are already fully
// covered by validateSources' literal-key check, so the common paths pay nothing.
func checkResolvedSchemaCollisions(ctx context.Context, url string, sources []Source) error {
	var hasDefault, hasExplicit bool
	for _, s := range sources {
		if s.Schema == "" {
			hasDefault = true
		} else {
			hasExplicit = true
		}
	}
	if !hasDefault || !hasExplicit {
		return nil
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("open database to resolve default schema: %w", err)
	}
	defer db.Close()

	var current sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&current); err != nil {
		return fmt.Errorf("resolve default schema: %w", err)
	}
	if !current.Valid {
		// search_path resolves to no existing schema; "" sources have no resolved
		// schema to collide on yet (their tables would fail to create later anyway).
		return nil
	}

	seen := make(map[string]string, len(sources))
	for _, s := range sources {
		schema := s.Schema
		if schema == "" {
			schema = current.String
		}
		key := schema + "\x00" + s.TrackingTable
		if other, ok := seen[key]; ok {
			return fmt.Errorf("sources %q and %q resolve to the same tracking table %q in schema %q (an empty Schema resolves to current_schema() = %q)",
				other, s.Name, s.TrackingTable, schema, current.String)
		}
		seen[key] = s.Name
	}
	return nil
}

// applySource runs Up for one source and rolls it back on failure. If prevVersion
// is 0 (no migrations have ever been applied) rollback is skipped to avoid
// dropping pre-existing tables on a GORM-to-golang-migrate upgrade. It returns
// the pre-run version so the caller can compensate this source if a later one fails.
func applySource(ctx context.Context, url string, src Source) (prevVersion uint, err error) {
	mg, err := newMigrate(ctx, url, src)
	if err != nil {
		return 0, err
	}
	defer closeMigrate(src.Name, mg)

	var dirty bool
	prevVersion, dirty, err = mg.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, fmt.Errorf("get pre-migration version for %s: %w", src.Name, err)
	}
	// prevVersion == 0 when ErrNilVersion (no migrations applied yet).

	// If the database is ahead of this binary's max known version, skip Up
	// entirely. The expand-then-contract policy in database-migrations.md
	// guarantees that each release's code is compatible with the schema applied
	// by the previous release, so rolling back one release at a time is safe.
	// We cannot enforce a tighter constraint here because migration version
	// numbers don't align with release versions.
	// A dirty database is excluded: dirty state means a previous migration
	// attempt failed and must be resolved, not silently accepted.
	if maxVer, scanErr := maxEmbeddedVersion(src.FS, src.Dir); scanErr != nil {
		log.Error(scanErr, "could not determine max embedded migration version; proceeding with Up", "track", src.Name)
	} else if prevVersion > maxVer {
		if dirty {
			// DB is both dirty and ahead of this binary. Attempting Up/rollback would
			// fail (the migration files for prevVersion don't exist), producing noisy
			// and misleading logs. Return a clear error so operators act on the real
			// problem rather than chasing rollback noise.
			return prevVersion, fmt.Errorf("database is dirty at version %d and ahead of this binary's max known version %d for track %s: manual operator intervention required: %w",
				prevVersion, maxVer, src.Name, migrate.ErrDirty{Version: int(prevVersion)})
		}
		log.Info("database schema is ahead of this binary; running in compatibility mode",
			"track", src.Name, "dbVersion", prevVersion, "binaryMax", maxVer)
		return prevVersion, nil
	}

	if upErr := mg.Up(); upErr != nil {
		if errors.Is(upErr, migrate.ErrNoChange) {
			return prevVersion, nil
		}
		if prevVersion == 0 {
			log.Info("migration failed; skipping rollback to version 0 to protect pre-existing data", "track", src.Name)
		} else {
			log.Info("migration failed, attempting rollback", "track", src.Name, "targetVersion", prevVersion)
			if rbErr := rollbackToVersion(mg, src.Name, prevVersion); rbErr != nil {
				log.Error(rbErr, "rollback failed", "track", src.Name)
			} else {
				log.Info("rollback complete", "track", src.Name, "version", prevVersion)
			}
		}
		return prevVersion, fmt.Errorf("run migrations for %s: %w", src.Name, upErr)
	}
	return prevVersion, nil
}

// rollbackSource opens a fresh migrate instance and rolls a source back to
// targetVersion. Used to compensate a previously-succeeded source when a later
// source fails. It returns an error (also logged) when the rollback fails, so
// the orchestrator can surface that the database may be left partially rolled
// back rather than leaving it as a log-only signal.
func rollbackSource(ctx context.Context, url string, src Source, targetVersion uint) error {
	mg, err := newMigrate(ctx, url, src)
	if err != nil {
		log.Error(err, "rollback failed (open)", "track", src.Name)
		return fmt.Errorf("open %s for rollback: %w", src.Name, err)
	}
	defer closeMigrate(src.Name, mg)
	if err := rollbackToVersion(mg, src.Name, targetVersion); err != nil {
		log.Error(err, "rollback failed", "track", src.Name)
		return fmt.Errorf("roll back %s to version %d: %w", src.Name, targetVersion, err)
	}
	log.Info("rollback complete", "track", src.Name, "version", targetVersion)
	return nil
}

// rollbackToVersion rolls the migration state back to targetVersion.
// It handles the dirty-state cleanup golang-migrate requires after a failed
// Up run before down steps can be applied.
func rollbackToVersion(mg *migrate.Migrate, name string, targetVersion uint) error {
	currentVersion, dirty, err := mg.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return nil // nothing was applied; nothing to roll back
		}
		return fmt.Errorf("get version after failure for %s: %w", name, err)
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
			return fmt.Errorf("clear dirty state for %s: %w", name, err)
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
		return fmt.Errorf("roll back %d step(s) for %s: %w", steps, name, err)
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

// newMigrate opens a database handle (sql.Open with the pgx stdlib shim) and
// constructs a migrate.Migrate for the given source. The caller must call
// closeMigrate when done.
//
// sql.Open returns a *sql.DB pool, but the session-scoped advisory lock and the
// migration run are safe regardless: the migratepgx driver checks out a single
// dedicated *sql.Conn and pins all lock/migration work to it. The schema setup
// below (CREATE SCHEMA, and the driver's current_schema() probe) may run on any
// pooled connection, which is fine because the schema name is quoted and
// search_path is set on the DSN — so every pooled connection targets the same
// schema. The orchestrator deliberately does not cap the pool to one connection.
//
// When src.Schema is set, the connection's search_path is pinned to that schema
// (so migration DDL lands there) and the schema is created if missing. The
// tracking table is also scoped to the schema via migratepgx.Config.SchemaName.
func newMigrate(ctx context.Context, dbURL string, src Source) (*migrate.Migrate, error) {
	connURL := dbURL
	if src.Schema != "" {
		if err := validateSchemaName(src.Schema); err != nil {
			return nil, err
		}
		var err error
		connURL, err = withSearchPath(dbURL, src.Schema)
		if err != nil {
			return nil, fmt.Errorf("set search_path for %s: %w", src.Name, err)
		}
	}

	db, err := sql.Open("pgx", connURL)
	if err != nil {
		return nil, fmt.Errorf("open database for %s: %w", src.Name, err)
	}

	if src.Schema != "" {
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+quoteIdentifier(src.Schema)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create schema %q for %s: %w", src.Schema, src.Name, err)
		}
	}

	srcDriver, err := iofs.New(src.FS, src.Dir)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("load migration files from %s: %w", src.Dir, err)
	}

	cfg := &migratepgx.Config{MigrationsTable: src.TrackingTable}
	if src.Schema != "" {
		cfg.SchemaName = src.Schema
	}
	driver, err := migratepgx.WithInstance(db, cfg)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create migration driver for %s: %w", src.Name, err)
	}

	mg, err := migrate.NewWithInstance("iofs", srcDriver, "postgres", driver)
	if err != nil {
		_ = srcDriver.Close()
		_ = db.Close()
		return nil, fmt.Errorf("create migrator for %s: %w", src.Name, err)
	}
	return mg, nil
}

// withSearchPath returns dbURL with the search_path connection parameter set to
// schema, so every connection in the pool (including the one golang-migrate
// checks out) targets that schema for migration DDL. dbURL must be a URL-form
// DSN (postgres://...); a libpq keyword/value DSN is rejected because net/url
// parses it without error into a meaningless URL, which would silently corrupt
// the connection string rather than fail here.
func withSearchPath(dbURL, schema string) (string, error) {
	u, err := nurl.Parse(dbURL)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	if u.Scheme == "" {
		return "", fmt.Errorf("database url must be a URL-form DSN (postgres://...) to scope a schema; got a non-URL DSN")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// schemaNameRe constrains a schema name to a lowercase identifier. The name is
// used both quoted (CREATE SCHEMA, the tracking table's SchemaName) and unquoted
// (the search_path connection parameter); Postgres case-folds the unquoted form,
// so a mixed-case name like "MySchema" would create the quoted schema "MySchema"
// while search_path resolved to the folded "myschema" and never matched. Keeping
// the name lowercase makes the two forms identical.
var schemaNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// validateSchemaName rejects schema identifiers that are not safe to interpolate
// into DDL. Schema names come from downstream-registered Sources and cannot be
// passed as bind parameters, so they are constrained to a conservative pattern.
func validateSchemaName(schema string) error {
	if len(schema) == 0 || len(schema) > 63 {
		return fmt.Errorf("invalid schema name %q: must be 1-63 characters", schema)
	}
	if !schemaNameRe.MatchString(schema) {
		return fmt.Errorf("invalid schema name %q: must match %s", schema, schemaNameRe.String())
	}
	return nil
}

// quoteIdentifier double-quotes a SQL identifier, escaping embedded quotes.
func quoteIdentifier(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}

// maxEmbeddedVersion scans dir inside migrationsFS and returns the highest migration
// version number found. Only files with a ".up.sql" suffix are considered. Version
// numbers are parsed from the leading decimal digits of each filename; the remainder
// of the name is not validated. Returns an error if the directory cannot be read or
// contains no recognisable migration files.
func maxEmbeddedVersion(migrationsFS fs.FS, dir string) (uint, error) {
	entries, err := fs.ReadDir(migrationsFS, dir)
	if err != nil {
		return 0, fmt.Errorf("read migration dir %s: %w", dir, err)
	}
	var highest uint
	var foundUpSQL, foundVersioned bool
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		foundUpSQL = true
		var v uint
		if _, scanErr := fmt.Sscanf(e.Name(), "%d", &v); scanErr != nil {
			continue
		}
		foundVersioned = true
		if v > highest {
			highest = v
		}
	}
	if !foundUpSQL {
		return 0, fmt.Errorf("no .up.sql migration files found in %s", dir)
	}
	if !foundVersioned {
		return 0, fmt.Errorf("no versioned .up.sql migration files found in %s; expected names like 000001_description.up.sql", dir)
	}
	return highest, nil
}

// closeMigrate closes mg, logging source and database close errors separately.
func closeMigrate(name string, mg *migrate.Migrate) {
	srcErr, dbErr := mg.Close()
	if srcErr != nil {
		log.Error(srcErr, "closing migration source", "track", name)
	}
	if dbErr != nil {
		log.Error(dbErr, "closing migration database", "track", name)
	}
}
