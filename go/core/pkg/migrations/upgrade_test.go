package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// subsetCoreFS returns a synthetic FS containing only the core migration files
// up to and including maxVersion. Used to drive applyDir to a specific
// schema version so a test can stage data at the corresponding code revision
// before applying further migrations.
func subsetCoreFS(t *testing.T, src fs.FS, maxVersion int) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(src, "core")
	if err != nil {
		t.Fatalf("subsetCoreFS: read dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var v int
		// Filenames are <NNNNNN>_<name>.up.sql / .down.sql.
		if _, err := fmt.Sscanf(e.Name(), "%06d_", &v); err != nil || v > maxVersion {
			continue
		}
		data, err := fs.ReadFile(src, "core/"+e.Name())
		if err != nil {
			t.Fatalf("subsetCoreFS: read %s: %v", e.Name(), err)
		}
		out["core/"+e.Name()] = &fstest.MapFile{Data: data}
	}
	if len(out) == 0 {
		t.Fatalf("subsetCoreFS: no migrations <= %d found", maxVersion)
	}
	return out
}

// TestUpgradePreservesAgentDataAcrossWorkloadTypeBackfill verifies that
// the data-modifying migration 000003_agent_workload_type preserves
// pre-existing agent rows and applies the 'deployment' default to legacy
// rows. Mirrors the data-preserving upgrade scenario from #1637.
//
// This is a starter for the data-preserving half of #1637; the rolling
// upgrade scenario (previous-release code against new schema) needs version
// pinning infrastructure not provided here.
func TestUpgradePreservesAgentDataAcrossWorkloadTypeBackfill(t *testing.T) {
	connStr := startTestDB(t)

	// Stage 1: apply migrations 1..2 — pre-workload_type schema.
	pre := subsetCoreFS(t, FS, 2)
	if _, err := applyDir(connStr, pre, "core", "schema_migrations"); err != nil {
		t.Fatalf("applyDir pre: %v", err)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 2 {
		t.Fatalf("schema version after pre = %d, want 2", got)
	}

	// Confirm workload_type is not yet in the schema. If a future migration
	// reorders or renames things this test should fail loudly rather than
	// silently testing the wrong invariant.
	if columnExists(t, connStr, "agent", "workload_type") {
		t.Fatal("agent.workload_type should not exist before migration 000003")
	}

	// Stage 2: insert representative legacy rows. Use the columns available at
	// schema version 2 — id, type, config — and leave timestamps NULL to mirror
	// the broadest legacy shape.
	seed := []struct {
		id, agentType, config string
	}{
		{"agent-alpha", "Declarative", `{"systemMessage":"alpha"}`},
		{"agent-beta", "Declarative", `{"systemMessage":"beta"}`},
		{"agent-gamma", "BYO", `{}`},
	}
	insertAgents(t, connStr, seed)

	// Stage 3: apply migration 000003. The migration adds workload_type, runs
	// `UPDATE agent SET workload_type = 'deployment' WHERE workload_type IS NULL`,
	// then sets the column NOT NULL with default 'deployment'.
	post := subsetCoreFS(t, FS, 3)
	if _, err := applyDir(connStr, post, "core", "schema_migrations"); err != nil {
		t.Fatalf("applyDir post: %v", err)
	}
	if got := trackVersion(t, connStr, "schema_migrations"); got != 3 {
		t.Fatalf("schema version after post = %d, want 3", got)
	}

	// Stage 4: verify every seeded row survived and was backfilled.
	rows := selectAgentsByID(t, connStr, idsOf(seed))
	if len(rows) != len(seed) {
		t.Fatalf("got %d rows after upgrade, want %d (data lost)", len(rows), len(seed))
	}
	for _, row := range rows {
		if row.WorkloadType != "deployment" {
			t.Errorf("agent %s: workload_type = %q, want %q (backfill missed legacy row)",
				row.ID, row.WorkloadType, "deployment")
		}
		// Sanity-check that other columns survived intact.
		want, ok := findSeed(seed, row.ID)
		if !ok {
			t.Errorf("agent %s: not in seed set", row.ID)
			continue
		}
		if row.Type != want.agentType {
			t.Errorf("agent %s: type = %q, want %q", row.ID, row.Type, want.agentType)
		}
	}
}

// --- helpers ---

func columnExists(t *testing.T, connStr, table, column string) bool {
	t.Helper()
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("columnExists: open db: %v", err)
	}
	defer db.Close()
	var exists bool
	err = db.QueryRowContext(context.Background(),
		`SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	if err != nil {
		t.Fatalf("columnExists: query: %v", err)
	}
	return exists
}

func insertAgents(t *testing.T, connStr string, rows []struct{ id, agentType, config string }) {
	t.Helper()
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("insertAgents: open db: %v", err)
	}
	defer db.Close()
	for _, r := range rows {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO agent (id, type, config) VALUES ($1, $2, $3::json)`,
			r.id, r.agentType, r.config); err != nil {
			t.Fatalf("insertAgents %s: %v", r.id, err)
		}
	}
}

type agentRow struct {
	ID, Type, WorkloadType string
}

func selectAgentsByID(t *testing.T, connStr string, ids []string) []agentRow {
	t.Helper()
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("selectAgentsByID: open db: %v", err)
	}
	defer db.Close()
	// IN-list with placeholders.
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(
		`SELECT id, type, workload_type FROM agent WHERE id IN (%s) ORDER BY id`,
		strings.Join(placeholders, ","),
	)
	rows, err := db.QueryContext(context.Background(), q, args...)
	if err != nil {
		t.Fatalf("selectAgentsByID: query: %v", err)
	}
	defer rows.Close()
	var out []agentRow
	for rows.Next() {
		var r agentRow
		if err := rows.Scan(&r.ID, &r.Type, &r.WorkloadType); err != nil {
			t.Fatalf("selectAgentsByID: scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func idsOf(seed []struct{ id, agentType, config string }) []string {
	out := make([]string, len(seed))
	for i, s := range seed {
		out[i] = s.id
	}
	return out
}

func findSeed(seed []struct{ id, agentType, config string }, id string) (struct{ id, agentType, config string }, bool) {
	for _, s := range seed {
		if s.id == id {
			return s, true
		}
	}
	return struct{ id, agentType, config string }{}, false
}
