package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/jackc/pgx/v5/stdlib"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// --- migration fixtures ---

// goodCoreFS has two valid core migrations.
var goodCoreFS = fstest.MapFS{
	"core/000001_create.up.sql":   {Data: []byte(`CREATE TABLE mig_test (id SERIAL PRIMARY KEY);`)},
	"core/000001_create.down.sql": {Data: []byte(`DROP TABLE IF EXISTS mig_test;`)},
	"core/000002_alter.up.sql":    {Data: []byte(`ALTER TABLE mig_test ADD COLUMN name TEXT;`)},
	"core/000002_alter.down.sql":  {Data: []byte(`ALTER TABLE mig_test DROP COLUMN IF EXISTS name;`)},
}

// oneCoreFS is just the first migration from goodCoreFS.
var oneCoreFS = fstest.MapFS{
	"core/000001_create.up.sql":   {Data: []byte(`CREATE TABLE mig_test (id SERIAL PRIMARY KEY);`)},
	"core/000001_create.down.sql": {Data: []byte(`DROP TABLE IF EXISTS mig_test;`)},
}

// failOnFirstCoreFS fails immediately on the first migration.
var failOnFirstCoreFS = fstest.MapFS{
	"core/000001_bad.up.sql":   {Data: []byte(`ALTER TABLE no_such_table ADD COLUMN x TEXT;`)},
	"core/000001_bad.down.sql": {Data: []byte(`SELECT 1;`)},
}

// failOnSecondCoreFS succeeds on migration 1 then fails on migration 2.
var failOnSecondCoreFS = fstest.MapFS{
	"core/000001_create.up.sql":   {Data: []byte(`CREATE TABLE mig_test (id SERIAL PRIMARY KEY);`)},
	"core/000001_create.down.sql": {Data: []byte(`DROP TABLE IF EXISTS mig_test;`)},
	"core/000002_bad.up.sql":      {Data: []byte(`ALTER TABLE no_such_table ADD COLUMN x TEXT;`)},
	"core/000002_bad.down.sql":    {Data: []byte(`SELECT 1;`)},
}

// failVectorFS has a vector migration that fails.
var failVectorFS = fstest.MapFS{
	"vector/000001_bad.up.sql":   {Data: []byte(`ALTER TABLE no_such_table ADD COLUMN y TEXT;`)},
	"vector/000001_bad.down.sql": {Data: []byte(`SELECT 1;`)},
}

// expandCoreFS creates shared_data with two columns. Used to test cross-track
// rollback scenarios where the vector track depends on this table.
var expandCoreFS = fstest.MapFS{
	"core/000001_create_shared.up.sql":   {Data: []byte(`CREATE TABLE IF NOT EXISTS shared_data (id SERIAL PRIMARY KEY, col_a TEXT);`)},
	"core/000001_create_shared.down.sql": {Data: []byte(`DROP TABLE IF EXISTS shared_data;`)},
	"core/000002_add_col_b.up.sql":       {Data: []byte(`ALTER TABLE shared_data ADD COLUMN IF NOT EXISTS col_b TEXT;`)},
	"core/000002_add_col_b.down.sql":     {Data: []byte(`ALTER TABLE shared_data DROP COLUMN IF EXISTS col_b;`)},
}

// failVectorWithDependencyFS is a vector migration that partially succeeds
// (adds a column to shared_data) then fails. Its down migration uses IF EXISTS
// so rollback is safe even if the column was never added.
var failVectorWithDependencyFS = fstest.MapFS{
	"vector/000001_bad_depends_on_core.up.sql":   {Data: []byte(`ALTER TABLE shared_data ADD COLUMN IF NOT EXISTS vec_col VECTOR(3); ALTER TABLE no_such_table ADD COLUMN x TEXT;`)},
	"vector/000001_bad_depends_on_core.down.sql": {Data: []byte(`ALTER TABLE shared_data DROP COLUMN IF EXISTS vec_col;`)},
}

// goodVectorFS has a valid vector migration.
var goodVectorFS = fstest.MapFS{
	"vector/000001_create.up.sql":   {Data: []byte(`CREATE EXTENSION IF NOT EXISTS vector; CREATE TABLE IF NOT EXISTS vec_test (id SERIAL PRIMARY KEY, embedding vector(3));`)},
	"vector/000001_create.down.sql": {Data: []byte(`DROP TABLE IF EXISTS vec_test; DROP EXTENSION IF EXISTS vector;`)},
}

// mergeFS combines multiple MapFS values into one.
func mergeFS(fsMaps ...fstest.MapFS) fstest.MapFS {
	out := fstest.MapFS{}
	for _, m := range fsMaps {
		maps.Copy(out, m)
	}
	return out
}

// coreSource builds a core Source backed by fsys (subdir "core").
func coreSource(fsys fstest.MapFS) Source {
	return Source{Name: "core", TrackingTable: "schema_migrations", FS: fsys, Dir: "core"}
}

// vectorSource builds a vector Source backed by fsys (subdir "vector"), with the
// pgvector precheck wired in to mirror BuiltinSources.
func vectorSource(fsys fstest.MapFS) Source {
	return Source{Name: "vector", TrackingTable: "vector_schema_migrations", FS: fsys, Dir: "vector", PreCheck: checkPgvector}
}

// trackVersion reads the current version from a golang-migrate tracking table.
// Returns 0 if the table is empty or does not exist (fully rolled back).
func trackVersion(t *testing.T, connStr, table string) uint {
	t.Helper()
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("trackVersion: open db: %v", err)
	}
	defer db.Close()
	var v uint
	err = db.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT version FROM %s LIMIT 1`, table)).Scan(&v)
	if err != nil {
		return 0 // sql.ErrNoRows or table doesn't exist
	}
	return v
}

// startTestDB spins up a pgvector Postgres container and returns its connection
// string, registering cleanup with t. It does not run any migrations.
func startTestDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
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
		t.Fatalf("startTestDB: start container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("startTestDB: connection string: %v", err)
	}
	return connStr
}

// startTestDBWithoutPgvector spins up a plain Postgres container (no pgvector)
// and returns its connection string, registering cleanup with t.
func startTestDBWithoutPgvector(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:18",
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
		t.Fatalf("startTestDBWithoutPgvector: start container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("startTestDBWithoutPgvector: connection string: %v", err)
	}
	return connStr
}

// tableExists checks whether a table exists in the public schema.
func tableExists(t *testing.T, connStr, table string) bool {
	t.Helper()
	return tableExistsInSchema(t, connStr, "public", table)
}

// tableExistsInSchema checks whether a table exists in the given schema.
func tableExistsInSchema(t *testing.T, connStr, schema, table string) bool {
	t.Helper()
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("tableExistsInSchema: open db: %v", err)
	}
	defer db.Close()
	var exists bool
	err = db.QueryRowContext(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)",
		schema, table).Scan(&exists)
	if err != nil {
		t.Fatalf("tableExistsInSchema: query: %v", err)
	}
	return exists
}

// --- applySource tests ---

func TestApplySource_HappyPath(t *testing.T) {
	connStr := startTestDB(t)

	prev, err := applySource(context.Background(), connStr, coreSource(goodCoreFS))
	if err != nil {
		t.Fatalf("applySource: %v", err)
	}
	if prev != 0 {
		t.Errorf("prevVersion = %d, want 0", prev)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("version = %d, want 2", got)
	}
}

func TestApplySource_NoOpWhenAlreadyAtLatest(t *testing.T) {
	connStr := startTestDB(t)

	if _, err := applySource(context.Background(), connStr, coreSource(goodCoreFS)); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	prev, err := applySource(context.Background(), connStr, coreSource(goodCoreFS))
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if prev != 2 {
		t.Errorf("prevVersion on no-op = %d, want 2", prev)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("version = %d, want 2", got)
	}
}

func TestApplySource_NoRollbackWhenFirstMigrationFails(t *testing.T) {
	connStr := startTestDB(t)

	if _, err := applySource(context.Background(), connStr, coreSource(failOnFirstCoreFS)); err == nil {
		t.Fatal("expected error, got nil")
	}
	// prevVersion was 0 so rollback is skipped to protect pre-existing data.
	// golang-migrate marks version 1 as dirty (the failed migration).
	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Errorf("version after failure = %d, want 1 (dirty, rollback skipped)", got)
	}
}

func TestApplySource_NoRollbackWhenLaterMigrationFails(t *testing.T) {
	connStr := startTestDB(t)

	if _, err := applySource(context.Background(), connStr, coreSource(failOnSecondCoreFS)); err == nil {
		t.Fatal("expected error, got nil")
	}
	// Migration 1 succeeded, migration 2 failed. Rollback is skipped because
	// prevVersion was 0. golang-migrate marks version 2 as dirty.
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("version after failure = %d, want 2 (dirty, rollback skipped)", got)
	}
}

func TestApplySource_RollsBackToExistingVersion(t *testing.T) {
	connStr := startTestDB(t)

	// Establish a baseline at version 1.
	if _, err := applySource(context.Background(), connStr, coreSource(oneCoreFS)); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Advance to version 2 — should fail and roll back to version 1, not 0.
	if _, err := applySource(context.Background(), connStr, coreSource(failOnSecondCoreFS)); err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Errorf("version after rollback = %d, want 1 (pre-run baseline)", got)
	}
}

// TestApplySource_RollsBackWithExistingVersion verifies that when migrations have
// previously been applied (prevVersion > 0), rollback always happens on failure.
// This ensures the rollback protection only affects the initial migration run
// (prevVersion == 0), not subsequent upgrades.
func TestApplySource_RollsBackWithExistingVersion(t *testing.T) {
	connStr := startTestDB(t)

	// Establish a baseline at version 1.
	if _, err := applySource(context.Background(), connStr, coreSource(oneCoreFS)); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Verify data exists at version 1.
	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Fatalf("setup: version = %d, want 1", got)
	}

	// Advance to version 2 — should roll back because prevVersion > 0.
	if _, err := applySource(context.Background(), connStr, coreSource(failOnSecondCoreFS)); err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Errorf("version after rollback = %d, want 1 (rollback should happen when prevVersion > 0)", got)
	}
}

func TestMaxEmbeddedVersion(t *testing.T) {
	tests := []struct {
		name    string
		fs      fstest.MapFS
		dir     string
		want    uint
		wantErr bool
	}{
		{
			name: "returns highest version from up.sql files",
			fs: fstest.MapFS{
				"core/000001_a.up.sql":   {},
				"core/000001_a.down.sql": {},
				"core/000003_b.up.sql":   {},
				"core/000003_b.down.sql": {},
			},
			dir:  "core",
			want: 3,
		},
		{
			name: "ignores non-sql files",
			fs: fstest.MapFS{
				"core/README.md":         {},
				"core/000002_x.up.sql":   {},
				"core/000002_x.down.sql": {},
			},
			dir:  "core",
			want: 2,
		},
		{
			name: "ignores down.sql when computing max",
			fs: fstest.MapFS{
				"core/000001_a.up.sql":   {},
				"core/000002_b.down.sql": {},
			},
			dir:  "core",
			want: 1,
		},
		{
			name:    "up.sql files with unparseable names returns error",
			fs:      fstest.MapFS{"core/init.up.sql": {}},
			dir:     "core",
			wantErr: true,
		},
		{
			name:    "empty dir returns error",
			fs:      fstest.MapFS{"core/.keep": {}},
			dir:     "core",
			wantErr: true,
		},
		{
			name:    "nonexistent dir returns error",
			fs:      fstest.MapFS{},
			dir:     "missing",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := maxEmbeddedVersion(tt.fs, tt.dir)
			if (err != nil) != tt.wantErr {
				t.Errorf("maxEmbeddedVersion() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("maxEmbeddedVersion() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestApplySource_SucceedsWhenDBVersionAhead verifies that an older binary starting against
// a database that a newer binary has migrated does not crash-loop. It skips Up entirely
// and returns success, leaving the schema unchanged. Safe rollback relies on the
// expand-then-contract discipline in database-migrations.md and rolling back one release
// at a time.
func TestApplySource_SucceedsWhenDBVersionAhead(t *testing.T) {
	connStr := startTestDB(t)

	// Newer binary applies v1 and v2.
	if _, err := applySource(context.Background(), connStr, coreSource(goodCoreFS)); err != nil {
		t.Fatalf("newer binary apply: %v", err)
	}

	// Older binary (max v1) starts against the v2 schema — must not error.
	if _, err := applySource(context.Background(), connStr, coreSource(oneCoreFS)); err != nil {
		t.Fatalf("older binary apply against newer schema: %v", err)
	}

	// Schema version must be unchanged — the older binary has no business rolling back.
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("version = %d, want 2 (older binary must not modify schema version)", got)
	}
}

// TestApplySource_DirtyStateNotMaskedByCompatibilityMode verifies that a dirty database
// is not silently accepted by compatibility mode. If the DB is both dirty and ahead of
// the binary's max known version, the dirty state must still be surfaced as an error.
func TestApplySource_DirtyStateNotMaskedByCompatibilityMode(t *testing.T) {
	connStr := startTestDB(t)

	// Apply v1 cleanly first so the tracking table exists.
	if _, err := applySource(context.Background(), connStr, coreSource(oneCoreFS)); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Simulate a newer binary having applied v2 but leaving it dirty.
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE schema_migrations SET version = 2, dirty = true"); err != nil {
		t.Fatalf("set dirty state: %v", err)
	}

	// Older binary (max v1) starts: DB is at v2 dirty. Compatibility mode must NOT
	// trigger — dirty state must be returned as an error so the operator can act.
	_, err = applySource(context.Background(), connStr, coreSource(oneCoreFS))
	if err == nil {
		t.Fatal("expected error for dirty database, got nil")
	}
	var dirtyErr migrate.ErrDirty
	if !errors.As(err, &dirtyErr) {
		t.Errorf("expected migrate.ErrDirty, got %T: %v", err, err)
	}
}

// --- rollbackSource tests ---

func TestRollbackSource_RollsBackToTarget(t *testing.T) {
	connStr := startTestDB(t)

	if _, err := applySource(context.Background(), connStr, coreSource(goodCoreFS)); err != nil {
		t.Fatalf("setup: %v", err)
	}

	rollbackSource(context.Background(), connStr, coreSource(goodCoreFS), 0)

	if got := trackVersion(t, connStr, "schema_migrations"); got != 0 {
		t.Errorf("version after rollback = %d, want 0", got)
	}
}

func TestRollbackSource_PartialRollback(t *testing.T) {
	connStr := startTestDB(t)

	if _, err := applySource(context.Background(), connStr, coreSource(goodCoreFS)); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Roll back only one step (2 → 1).
	rollbackSource(context.Background(), connStr, coreSource(goodCoreFS), 1)

	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Errorf("version after partial rollback = %d, want 1", got)
	}
}

// --- cross-track rollback (per-source primitives) ---

// TestCrossTrackRollback_CoreUnchangedWhenVectorFails covers the case where
// core has no new migrations (ErrNoChange) and vector fails. Core should not
// be downgraded by the cross-track rollback.
func TestCrossTrackRollback_CoreUnchangedWhenVectorFails(t *testing.T) {
	connStr := startTestDB(t)

	combined := mergeFS(goodCoreFS, failVectorFS)

	// Establish core at its latest version before the run.
	if _, err := applySource(context.Background(), connStr, coreSource(combined)); err != nil {
		t.Fatalf("setup core: %v", err)
	}

	// Core has no new migrations — applySource returns ErrNoChange.
	corePrev, err := applySource(context.Background(), connStr, coreSource(combined))
	if err != nil {
		t.Fatalf("core apply (no-op): %v", err)
	}
	if corePrev != 2 {
		t.Fatalf("corePrev = %d, want 2", corePrev)
	}

	// Vector fails and self-rolls-back.
	if _, err := applySource(context.Background(), connStr, vectorSource(combined)); err == nil {
		t.Fatal("expected vector error, got nil")
	}

	// Cross-track rollback: core should be untouched since corePrev == current version.
	rollbackSource(context.Background(), connStr, coreSource(combined), corePrev)
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("core version = %d, want 2 (should not have been downgraded)", got)
	}
}

func TestCrossTrackRollback_CoreRolledBackWhenVectorFails(t *testing.T) {
	connStr := startTestDB(t)

	combined := mergeFS(goodCoreFS, failVectorFS)

	// Core succeeds.
	corePrev, err := applySource(context.Background(), connStr, coreSource(combined))
	if err != nil {
		t.Fatalf("core apply: %v", err)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Fatalf("core version = %d, want 2", got)
	}

	// Vector fails. Self-rollback is skipped because vector prevVersion is 0.
	if _, err := applySource(context.Background(), connStr, vectorSource(combined)); err == nil {
		t.Fatal("expected vector error, got nil")
	}
	if got := trackVersion(t, connStr, "vector_schema_migrations"); got != 1 {
		t.Errorf("vector version after failure = %d, want 1 (dirty, rollback skipped)", got)
	}

	// Cross-track rollback: core should be rolled back to its pre-run version.
	rollbackSource(context.Background(), connStr, coreSource(combined), corePrev)
	if got := trackVersion(t, connStr, "schema_migrations"); got != corePrev {
		t.Errorf("core version after cross-track rollback = %d, want %d", got, corePrev)
	}
}

// TestCrossTrackRollback_IfExistsGuardsSafeOnVectorFailure verifies that when a
// vector migration fails and triggers a core cross-track rollback, the IF EXISTS
// guards in both down migrations prevent errors even though the vector migration
// only partially applied and shared_data is being dropped by core's rollback.
func TestCrossTrackRollback_IfExistsGuardsSafeOnVectorFailure(t *testing.T) {
	connStr := startTestDB(t)

	combined := mergeFS(expandCoreFS, failVectorWithDependencyFS)

	// Core succeeds (shared_data created with col_a and col_b).
	corePrev, err := applySource(context.Background(), connStr, coreSource(combined))
	if err != nil {
		t.Fatalf("core apply: %v", err)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Fatalf("core version = %d, want 2", got)
	}

	// Vector fails. Self-rollback is skipped because vector prevVersion is 0.
	if _, err := applySource(context.Background(), connStr, vectorSource(combined)); err == nil {
		t.Fatal("expected vector error, got nil")
	}
	if got := trackVersion(t, connStr, "vector_schema_migrations"); got != 1 {
		t.Errorf("vector version after failure = %d, want 1 (dirty, rollback skipped)", got)
	}

	// Cross-track rollback: core rolls back to its pre-run version.
	rollbackSource(context.Background(), connStr, coreSource(combined), corePrev)
	if got := trackVersion(t, connStr, "schema_migrations"); got != corePrev {
		t.Errorf("core version after cross-track rollback = %d, want %d", got, corePrev)
	}
}

// --- checkPgvector tests ---

func TestCheckPgvector_SucceedsOnPgvectorDB(t *testing.T) {
	connStr := startTestDB(t) // pgvector image
	if err := checkPgvector(connStr); err != nil {
		t.Errorf("checkPgvector on pgvector db: %v", err)
	}
}

func TestCheckPgvector_FailsOnPlainPostgres(t *testing.T) {
	connStr := startTestDBWithoutPgvector(t) // plain postgres image
	if err := checkPgvector(connStr); err == nil {
		t.Error("checkPgvector on plain postgres: expected error, got nil")
	}
}

// --- RunUp end-to-end tests ---

func TestRunUp_CoreAndVector(t *testing.T) {
	connStr := startTestDB(t)
	combined := mergeFS(goodCoreFS, goodVectorFS)

	if err := RunUp(context.Background(), connStr, []Source{coreSource(combined), vectorSource(combined)}); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("core version = %d, want 2", got)
	}
	if got := trackVersion(t, connStr, "vector_schema_migrations"); got != 1 {
		t.Errorf("vector version = %d, want 1", got)
	}
}

func TestRunUp_CoreOnlyWhenVectorDisabled(t *testing.T) {
	connStr := startTestDB(t)
	combined := mergeFS(goodCoreFS, goodVectorFS)

	if err := RunUp(context.Background(), connStr, []Source{coreSource(combined)}); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("core version = %d, want 2", got)
	}
	// Vector tracking table should not exist.
	if tableExists(t, connStr, "vector_schema_migrations") {
		t.Error("vector_schema_migrations should not exist when vector source is not registered")
	}
}

func TestRunUp_FailsBeforeMigrationsWhenPgvectorMissing(t *testing.T) {
	connStr := startTestDBWithoutPgvector(t)

	err := RunUp(context.Background(), connStr, []Source{coreSource(goodCoreFS), vectorSource(goodCoreFS)})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Core migrations should NOT have run — no tracking table created. The vector
	// precheck runs up front, before any source is applied.
	if tableExists(t, connStr, "schema_migrations") {
		t.Error("schema_migrations should not exist — pgvector check should fail before any migrations")
	}
}

// TestRunUp_SkipsCoreRollbackWhenVectorFailsOnFirstRun verifies the cross-track
// rollback protection in RunUp: when vector fails and corePrev is 0 (initial run),
// core is not rolled back to protect pre-existing data.
func TestRunUp_SkipsCoreRollbackWhenVectorFailsOnFirstRun(t *testing.T) {
	connStr := startTestDB(t) // pgvector available so checkPgvector passes
	combined := mergeFS(goodCoreFS, failVectorFS)

	err := RunUp(context.Background(), connStr, []Source{coreSource(combined), vectorSource(combined)})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Core should still be at version 2 — not rolled back to 0.
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("core version = %d, want 2 (should not be rolled back when corePrev == 0)", got)
	}
}

// TestRunUp_MultiSourceOrdering verifies sources apply in slice order: source "b"
// references a table created by source "a", so it can only succeed if "a" ran first.
func TestRunUp_MultiSourceOrdering(t *testing.T) {
	connStr := startTestDB(t)

	ordFS := fstest.MapFS{
		"a/000001_a.up.sql":   {Data: []byte(`CREATE TABLE ord_a (id INT PRIMARY KEY);`)},
		"a/000001_a.down.sql": {Data: []byte(`DROP TABLE IF EXISTS ord_a;`)},
		"b/000001_b.up.sql":   {Data: []byte(`CREATE TABLE ord_b (id INT PRIMARY KEY, a_id INT REFERENCES ord_a(id));`)},
		"b/000001_b.down.sql": {Data: []byte(`DROP TABLE IF EXISTS ord_b;`)},
		"c/000001_c.up.sql":   {Data: []byte(`CREATE TABLE ord_c (id INT PRIMARY KEY);`)},
		"c/000001_c.down.sql": {Data: []byte(`DROP TABLE IF EXISTS ord_c;`)},
	}
	sources := []Source{
		{Name: "a", TrackingTable: "ord_a_migrations", FS: ordFS, Dir: "a"},
		{Name: "b", TrackingTable: "ord_b_migrations", FS: ordFS, Dir: "b"},
		{Name: "c", TrackingTable: "ord_c_migrations", FS: ordFS, Dir: "c"},
	}

	if err := RunUp(context.Background(), connStr, sources); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	for _, table := range []string{"ord_a", "ord_b", "ord_c"} {
		if !tableExists(t, connStr, table) {
			t.Errorf("table %s should exist", table)
		}
	}
	for _, tbl := range []string{"ord_a_migrations", "ord_b_migrations", "ord_c_migrations"} {
		if got := trackVersion(t, connStr, tbl); got != 1 {
			t.Errorf("%s version = %d, want 1", tbl, got)
		}
	}
}

// TestRunUp_CompensatingRollbackAcrossThreeSources verifies that when a later
// source fails, previously-applied sources are rolled back to their pre-run
// versions in reverse order — and that the prevVersion==0 guard skips a source
// applied for the first time. Source "a" is fresh (prev 0, not compensated);
// source "b" is pre-seeded at v1 (prev 1, rolled back); source "c" fails.
func TestRunUp_CompensatingRollbackAcrossThreeSources(t *testing.T) {
	connStr := startTestDB(t)

	aFS := fstest.MapFS{
		"a/000001_a.up.sql":   {Data: []byte(`CREATE TABLE comp_a (id INT PRIMARY KEY);`)},
		"a/000001_a.down.sql": {Data: []byte(`DROP TABLE IF EXISTS comp_a;`)},
	}
	bV1FS := fstest.MapFS{
		"b/000001_b.up.sql":   {Data: []byte(`CREATE TABLE comp_b (id INT PRIMARY KEY);`)},
		"b/000001_b.down.sql": {Data: []byte(`DROP TABLE IF EXISTS comp_b;`)},
	}
	bFullFS := fstest.MapFS{
		"b/000001_b.up.sql":          {Data: []byte(`CREATE TABLE comp_b (id INT PRIMARY KEY);`)},
		"b/000001_b.down.sql":        {Data: []byte(`DROP TABLE IF EXISTS comp_b;`)},
		"b/000002_b_addcol.up.sql":   {Data: []byte(`ALTER TABLE comp_b ADD COLUMN extra TEXT;`)},
		"b/000002_b_addcol.down.sql": {Data: []byte(`ALTER TABLE comp_b DROP COLUMN IF EXISTS extra;`)},
	}
	cFS := fstest.MapFS{
		"c/000001_bad.up.sql":   {Data: []byte(`ALTER TABLE no_such_table ADD COLUMN x TEXT;`)},
		"c/000001_bad.down.sql": {Data: []byte(`SELECT 1;`)},
	}

	aSrc := Source{Name: "a", TrackingTable: "comp_a_migrations", FS: aFS, Dir: "a"}
	bSrcFull := Source{Name: "b", TrackingTable: "comp_b_migrations", FS: bFullFS, Dir: "b"}
	cSrc := Source{Name: "c", TrackingTable: "comp_c_migrations", FS: cFS, Dir: "c"}

	// Pre-seed b at v1 so its prevVersion is > 0 during the run below.
	if _, err := applySource(context.Background(), connStr, Source{Name: "b", TrackingTable: "comp_b_migrations", FS: bV1FS, Dir: "b"}); err != nil {
		t.Fatalf("seed b: %v", err)
	}

	err := RunUp(context.Background(), connStr, []Source{aSrc, bSrcFull, cSrc})
	if err == nil {
		t.Fatal("expected error from failing source c, got nil")
	}

	// a: applied for the first time (prev 0) → compensation skipped → stays at v1.
	if got := trackVersion(t, connStr, "comp_a_migrations"); got != 1 {
		t.Errorf("comp_a version = %d, want 1 (prev==0 guard skips compensation)", got)
	}
	// b: prev was 1, advanced to 2, compensated back to 1.
	if got := trackVersion(t, connStr, "comp_b_migrations"); got != 1 {
		t.Errorf("comp_b version = %d, want 1 (rolled back to pre-run version)", got)
	}
}

// TestRunUp_SchemaScopedSource verifies a Source with a non-empty Schema creates
// the schema and lands both its objects and its tracking table there, not in public.
func TestRunUp_SchemaScopedSource(t *testing.T) {
	connStr := startTestDB(t)

	schemaFS := fstest.MapFS{
		"s/000001_create.up.sql":   {Data: []byte(`CREATE TABLE scoped_t (id INT PRIMARY KEY);`)},
		"s/000001_create.down.sql": {Data: []byte(`DROP TABLE IF EXISTS scoped_t;`)},
	}
	src := Source{Name: "scoped", Schema: "myschema", TrackingTable: "schema_migrations", FS: schemaFS, Dir: "s"}

	if err := RunUp(context.Background(), connStr, []Source{src}); err != nil {
		t.Fatalf("RunUp: %v", err)
	}

	if !tableExistsInSchema(t, connStr, "myschema", "scoped_t") {
		t.Error("scoped_t should exist in myschema")
	}
	if tableExistsInSchema(t, connStr, "public", "scoped_t") {
		t.Error("scoped_t should NOT exist in public")
	}
	if !tableExistsInSchema(t, connStr, "myschema", "schema_migrations") {
		t.Error("tracking table should exist in myschema")
	}
	if tableExistsInSchema(t, connStr, "public", "schema_migrations") {
		t.Error("tracking table should NOT exist in public")
	}
}

// TestRunUp_RejectsResolvedSchemaCollision verifies the runtime guard catches a
// collision validateSources cannot: an unscoped source (Schema "") and an explicit
// source naming the connection's current_schema() ("public" here) share one
// tracking table once "" resolves, so RunUp must reject the set before applying
// anything. validateSources alone passes them (distinct literal Schema keys).
func TestRunUp_RejectsResolvedSchemaCollision(t *testing.T) {
	connStr := startTestDB(t)

	collide := []Source{
		{Name: "implicit", Schema: "", TrackingTable: "schema_migrations", FS: goodCoreFS, Dir: "core"},
		{Name: "explicit", Schema: "public", TrackingTable: "schema_migrations", FS: goodCoreFS, Dir: "core"},
	}

	// validateSources keys on the literal Schema, so it does NOT catch this.
	if err := validateSources(collide); err != nil {
		t.Fatalf("validateSources should pass on distinct literal schemas, got %v", err)
	}

	// RunUp resolves "" to current_schema() (public) and must reject.
	err := RunUp(context.Background(), connStr, collide)
	if err == nil {
		t.Fatal("expected resolved-collision error, got nil")
	}
	if !strings.Contains(err.Error(), "resolve to the same tracking table") {
		t.Errorf("error %q should describe a resolved tracking-table collision", err)
	}
	// Guard runs before any source applies — no tracking table created.
	if tableExists(t, connStr, "schema_migrations") {
		t.Error("schema_migrations should not exist — guard must fire before any source applies")
	}
}

// TestRunUp_PreCheckRunsBeforeAnyApply verifies that a failing PreCheck on a later
// source aborts the run before any earlier source is applied.
func TestRunUp_PreCheckRunsBeforeAnyApply(t *testing.T) {
	connStr := startTestDB(t)

	preFS := fstest.MapFS{
		"p/000001_create.up.sql":   {Data: []byte(`CREATE TABLE precheck_t (id INT PRIMARY KEY);`)},
		"p/000001_create.down.sql": {Data: []byte(`DROP TABLE IF EXISTS precheck_t;`)},
	}
	first := Source{Name: "first", TrackingTable: "first_migrations", FS: preFS, Dir: "p"}
	second := Source{Name: "second", TrackingTable: "second_migrations", FS: preFS, Dir: "p",
		PreCheck: func(string) error { return fmt.Errorf("precheck boom") }}

	err := RunUp(context.Background(), connStr, []Source{first, second})
	if err == nil {
		t.Fatal("expected error from failing precheck, got nil")
	}
	if tableExists(t, connStr, "precheck_t") {
		t.Error("precheck_t should not exist — no source should apply when a precheck fails")
	}
	if tableExists(t, connStr, "first_migrations") {
		t.Error("first_migrations tracking table should not exist — first source must not have applied")
	}
}

func TestValidateSchemaName(t *testing.T) {
	valid := []string{"a", "myschema", "tenant_1", "_x", "s123"}
	for _, s := range valid {
		if err := validateSchemaName(s); err != nil {
			t.Errorf("validateSchemaName(%q) = %v, want nil", s, err)
		}
	}
	// Uppercase is rejected: the name is used unquoted in search_path (which
	// Postgres case-folds) and quoted in CREATE SCHEMA, so a mixed-case name
	// would split across two schemas.
	invalid := []string{"", "1abc", "has space", "a;b", `a"b`, "a-b", "a.b", "drop table", "ABC", "MySchema", strings.Repeat("x", 64)}
	for _, s := range invalid {
		if err := validateSchemaName(s); err == nil {
			t.Errorf("validateSchemaName(%q) = nil, want error", s)
		}
	}
}

// --- container-free unit tests ---

// TestValidateSources_RejectsDuplicateSchemaAndTable verifies the collision unit
// is (Schema, TrackingTable): two sources sharing both are rejected, while the
// same tracking-table name in different schemas, or different tables in the same
// schema, are allowed.
func TestValidateSources_RejectsDuplicateSchemaAndTable(t *testing.T) {
	dummy := fstest.MapFS{} // non-nil so the required-field checks pass

	collide := []Source{
		{Name: "a", Schema: "s1", TrackingTable: "schema_migrations", FS: dummy, Dir: "a"},
		{Name: "b", Schema: "s1", TrackingTable: "schema_migrations", FS: dummy, Dir: "b"},
	}
	if err := validateSources(collide); err == nil {
		t.Error("expected collision error for same (schema, tracking table), got nil")
	}

	ok := []Source{
		// Same table name, different schema — fine.
		{Name: "a", Schema: "s1", TrackingTable: "schema_migrations", FS: dummy, Dir: "a"},
		{Name: "b", Schema: "s2", TrackingTable: "schema_migrations", FS: dummy, Dir: "b"},
		// Different table, same (default) schema — fine.
		{Name: "core", TrackingTable: "schema_migrations", FS: dummy, Dir: "core"},
		{Name: "vector", TrackingTable: "vector_schema_migrations", FS: dummy, Dir: "vector"},
	}
	if err := validateSources(ok); err != nil {
		t.Errorf("validateSources(distinct sources) = %v, want nil", err)
	}

	// An invalid schema name on any source is rejected up front, before any
	// source applies (fail-fast).
	badSchema := []Source{
		{Name: "good", TrackingTable: "schema_migrations", FS: dummy, Dir: "core"},
		{Name: "bad", Schema: "MixedCase", TrackingTable: "schema_migrations", FS: dummy, Dir: "x"},
	}
	if err := validateSources(badSchema); err == nil {
		t.Error("expected error for invalid schema name on a later source, got nil")
	}
}

// TestValidateSources_RejectsMissingRequiredFields verifies Source's required
// fields (Name, TrackingTable, FS, Dir) are enforced up front, while Schema
// stays optional ("" = connection default).
func TestValidateSources_RejectsMissingRequiredFields(t *testing.T) {
	base := Source{Name: "x", TrackingTable: "x_migrations", FS: fstest.MapFS{}, Dir: "x"}

	// The fully-populated base (with empty Schema) is valid.
	if err := validateSources([]Source{base}); err != nil {
		t.Fatalf("validateSources(valid source) = %v, want nil", err)
	}

	mutations := map[string]func(Source) Source{
		"empty Name":          func(s Source) Source { s.Name = ""; return s },
		"empty TrackingTable": func(s Source) Source { s.TrackingTable = ""; return s },
		"nil FS":              func(s Source) Source { s.FS = nil; return s },
		"empty Dir":           func(s Source) Source { s.Dir = ""; return s },
	}
	for name, mut := range mutations {
		t.Run(name, func(t *testing.T) {
			if err := validateSources([]Source{mut(base)}); err == nil {
				t.Errorf("validateSources with %s = nil, want error", name)
			}
		})
	}
}

// TestRunUp_EmptyAndNilSources verifies the no-op fast paths run no migration
// logic and need no database.
func TestRunUp_EmptyAndNilSources(t *testing.T) {
	if err := RunUp(context.Background(), "postgres://invalid", nil); err != nil {
		t.Errorf("RunUp(nil sources) = %v, want nil", err)
	}
	if err := RunUp(context.Background(), "postgres://invalid", []Source{}); err != nil {
		t.Errorf("RunUp(empty sources) = %v, want nil", err)
	}
}

// TestWithSearchPath verifies a postgres:// or postgresql:// DSN gains the
// search_path param (preserving existing params) and that anything else is
// rejected rather than silently rewritten.
func TestWithSearchPath(t *testing.T) {
	for _, dsn := range []string{
		"postgres://u:p@host:5432/db?sslmode=disable",
		"postgresql://u:p@host:5432/db",
	} {
		got, err := withSearchPath(dsn, "myschema")
		if err != nil {
			t.Fatalf("withSearchPath(%q) = %v, want nil", dsn, err)
		}
		if !strings.Contains(got, "search_path=myschema") {
			t.Errorf("withSearchPath(%q) = %q, missing search_path=myschema", dsn, got)
		}
	}

	// Existing query params are preserved.
	if got, _ := withSearchPath("postgres://u:p@host:5432/db?sslmode=disable", "myschema"); !strings.Contains(got, "sslmode=disable") {
		t.Errorf("result %q dropped existing sslmode param", got)
	}

	// Non-postgres inputs fail fast rather than being silently rewritten.
	for _, bad := range []string{
		"host=localhost dbname=foo user=bar", // libpq keyword/value DSN (scheme "")
		"localhost:5432/db",                  // parses as scheme "localhost"
		"mysql://u:p@host/db",                // wrong scheme
		"",                                   // empty
	} {
		if _, err := withSearchPath(bad, "myschema"); err == nil {
			t.Errorf("withSearchPath(%q) = nil, want error", bad)
		}
	}
}

// --- dirty state recovery tests ---

// TestApplySource_DirtyStateRecoveryOnRestart simulates a restart after a failed
// migration left the database in a dirty state. On the second call, prevVersion
// is > 0 (the dirty version), so rollback is enabled. The runner should clear
// the dirty state and roll back to the last clean version.
func TestApplySource_DirtyStateRecoveryOnRestart(t *testing.T) {
	connStr := startTestDB(t)

	// First run: apply version 1, then version 2 fails. prevVersion is 0, so
	// rollback is skipped. Database left at version 2 dirty.
	if _, err := applySource(context.Background(), connStr, coreSource(failOnSecondCoreFS)); err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Fatalf("after first run: version = %d, want 2 (dirty)", got)
	}

	// Second run (simulating restart): prevVersion is now 2 (dirty). The runner
	// should detect dirty state and attempt to clear it. mg.Up() will fail because
	// the database is dirty, then rollbackToVersion clears dirty to version 1.
	_, err := applySource(context.Background(), connStr, coreSource(failOnSecondCoreFS))
	if err == nil {
		t.Fatal("expected error on second run, got nil")
	}
	// After rollback clears dirty state, version should be at 1 (last clean).
	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Errorf("after restart: version = %d, want 1 (dirty cleared, rolled back)", got)
	}
}
