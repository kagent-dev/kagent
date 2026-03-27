package main

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"testing"
	"testing/fstest"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	_ "github.com/lib/pq"
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

// mergeFS combines multiple MapFS values into one.
func mergeFS(fsMaps ...fstest.MapFS) fstest.MapFS {
	out := fstest.MapFS{}
	for _, m := range fsMaps {
		maps.Copy(out, m)
	}
	return out
}

// trackVersion reads the current version from a golang-migrate tracking table.
// Returns 0 if the table is empty or does not exist (fully rolled back).
func trackVersion(t *testing.T, connStr, table string) uint {
	t.Helper()
	db, err := sql.Open("postgres", connStr)
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

// --- applyDir tests ---

func TestApplyDir_HappyPath(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	prev, err := applyDir(connStr, goodCoreFS, "core", "schema_migrations")
	if err != nil {
		t.Fatalf("applyDir: %v", err)
	}
	if prev != 0 {
		t.Errorf("prevVersion = %d, want 0", prev)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("version = %d, want 2", got)
	}
}

func TestApplyDir_NoOpWhenAlreadyAtLatest(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	if _, err := applyDir(connStr, goodCoreFS, "core", "schema_migrations"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	prev, err := applyDir(connStr, goodCoreFS, "core", "schema_migrations")
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

func TestApplyDir_RollsBackWhenFirstMigrationFails(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	if _, err := applyDir(connStr, failOnFirstCoreFS, "core", "schema_migrations"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 0 {
		t.Errorf("version after rollback = %d, want 0", got)
	}
}

func TestApplyDir_RollsBackWhenLaterMigrationFails(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	if _, err := applyDir(connStr, failOnSecondCoreFS, "core", "schema_migrations"); err == nil {
		t.Fatal("expected error, got nil")
	}
	// Migration 1 succeeded then was rolled back along with the failed migration 2.
	if got := trackVersion(t, connStr, "schema_migrations"); got != 0 {
		t.Errorf("version after rollback = %d, want 0", got)
	}
}

func TestApplyDir_RollsBackToExistingVersion(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	// Establish a baseline at version 1.
	if _, err := applyDir(connStr, oneCoreFS, "core", "schema_migrations"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Advance to version 2 — should fail and roll back to version 1, not 0.
	if _, err := applyDir(connStr, failOnSecondCoreFS, "core", "schema_migrations"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Errorf("version after rollback = %d, want 1 (pre-run baseline)", got)
	}
}

// --- rollbackDir tests ---

func TestRollbackDir_RollsBackToTarget(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	if _, err := applyDir(connStr, goodCoreFS, "core", "schema_migrations"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	rollbackDir(connStr, goodCoreFS, "core", "schema_migrations", 0)

	if got := trackVersion(t, connStr, "schema_migrations"); got != 0 {
		t.Errorf("version after rollback = %d, want 0", got)
	}
}

func TestRollbackDir_PartialRollback(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	if _, err := applyDir(connStr, goodCoreFS, "core", "schema_migrations"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Roll back only one step (2 → 1).
	rollbackDir(connStr, goodCoreFS, "core", "schema_migrations", 1)

	if got := trackVersion(t, connStr, "schema_migrations"); got != 1 {
		t.Errorf("version after partial rollback = %d, want 1", got)
	}
}

// --- cross-track rollback ---

// TestCrossTrackRollback_CoreUnchangedWhenVectorFails covers the case where
// core has no new migrations (ErrNoChange) and vector fails. Core should not
// be downgraded by the cross-track rollback.
func TestCrossTrackRollback_CoreUnchangedWhenVectorFails(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	combined := mergeFS(goodCoreFS, failVectorFS)

	// Establish core at its latest version before the run.
	if _, err := applyDir(connStr, combined, "core", "schema_migrations"); err != nil {
		t.Fatalf("setup core: %v", err)
	}

	// Core has no new migrations — applyDir returns ErrNoChange.
	corePrev, err := applyDir(connStr, combined, "core", "schema_migrations")
	if err != nil {
		t.Fatalf("core apply (no-op): %v", err)
	}
	if corePrev != 2 {
		t.Fatalf("corePrev = %d, want 2", corePrev)
	}

	// Vector fails and self-rolls-back.
	if _, err := applyDir(connStr, combined, "vector", "vector_schema_migrations"); err == nil {
		t.Fatal("expected vector error, got nil")
	}

	// Cross-track rollback: core should be untouched since corePrev == current version.
	rollbackDir(connStr, combined, "core", "schema_migrations", corePrev)
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Errorf("core version = %d, want 2 (should not have been downgraded)", got)
	}
}

func TestCrossTrackRollback_CoreRolledBackWhenVectorFails(t *testing.T) {
	connStr := dbtest.StartT(context.Background(), t)

	combined := mergeFS(goodCoreFS, failVectorFS)

	// Core succeeds.
	corePrev, err := applyDir(connStr, combined, "core", "schema_migrations")
	if err != nil {
		t.Fatalf("core apply: %v", err)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Fatalf("core version = %d, want 2", got)
	}

	// Vector fails and rolls itself back.
	if _, err := applyDir(connStr, combined, "vector", "vector_schema_migrations"); err == nil {
		t.Fatal("expected vector error, got nil")
	}
	if got := trackVersion(t, connStr, "vector_schema_migrations"); got != 0 {
		t.Errorf("vector version after self-rollback = %d, want 0", got)
	}

	// Cross-track rollback: core should be rolled back to its pre-run version.
	rollbackDir(connStr, combined, "core", "schema_migrations", corePrev)
	if got := trackVersion(t, connStr, "schema_migrations"); got != corePrev {
		t.Errorf("core version after cross-track rollback = %d, want %d", got, corePrev)
	}
}
