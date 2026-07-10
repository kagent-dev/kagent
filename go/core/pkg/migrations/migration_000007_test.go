package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

// TestMigration000007_KindQualifiedIDs seeds pre-000007 rows (bare ids for all
// kinds, sessions pointing at them), applies the migration, and asserts only
// SandboxAgent/AgentHarness rows and their sessions are rewritten — then rolls
// back one step and asserts the exact original state.
func TestMigration000007_KindQualifiedIDs(t *testing.T) {
	connStr := startTestDB(t)
	ctx := context.Background()
	src := BuiltinSources(false)[0] // core track on the real embedded FS

	if err := WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Migrate(6)
	}); err != nil {
		t.Fatalf("migrate to version 6: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	seed := []struct{ id, typ string }{
		{"default__NS__foo", "Declarative"},       // plain Agent — untouched
		{"default__NS__box", "SandboxAgent"},      // rewritten
		{"default__NS__claw", "AgentHarness"},     // rewritten
		{"sandboxagents__NS__foo", "Declarative"}, // Agent in a namespace named "sandboxagents" — untouched
	}
	for _, s := range seed {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent (id, type) VALUES ($1, $2)`, s.id, s.typ); err != nil {
			t.Fatalf("seed agent %s: %v", s.id, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO session (id, user_id, agent_id) VALUES ($1, 'u1', $2)`, "session-"+s.id, s.id); err != nil {
			t.Fatalf("seed session for %s: %v", s.id, err)
		}
	}

	if err := WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Migrate(7)
	}); err != nil {
		t.Fatalf("apply 000007: %v", err)
	}

	assertIDs := func(want map[string]string) {
		t.Helper()
		for sessionID, wantAgentID := range want {
			var agentID string
			if err := db.QueryRowContext(ctx,
				`SELECT agent_id FROM session WHERE id = $1`, sessionID).Scan(&agentID); err != nil {
				t.Fatalf("read session %s: %v", sessionID, err)
			}
			if agentID != wantAgentID {
				t.Errorf("session %s agent_id = %q, want %q", sessionID, agentID, wantAgentID)
			}
			var one int
			if err := db.QueryRowContext(ctx,
				`SELECT 1 FROM agent WHERE id = $1`, wantAgentID).Scan(&one); err != nil {
				t.Errorf("agent row %q missing: %v", wantAgentID, err)
			}
		}
	}

	assertIDs(map[string]string{
		"session-default__NS__foo":       "default__NS__foo",
		"session-default__NS__box":       "sandboxagents__NS__default__NS__box",
		"session-default__NS__claw":      "agentharnesses__NS__default__NS__claw",
		"session-sandboxagents__NS__foo": "sandboxagents__NS__foo",
	})

	if err := WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Steps(-1)
	}); err != nil {
		t.Fatalf("roll back 000007: %v", err)
	}

	assertIDs(map[string]string{
		"session-default__NS__foo":       "default__NS__foo",
		"session-default__NS__box":       "default__NS__box",
		"session-default__NS__claw":      "default__NS__claw",
		"session-sandboxagents__NS__foo": "sandboxagents__NS__foo",
	})
}

// TestMigration000007_DownFailsOnSharedName pins the documented rollback limit:
// once an Agent and a SandboxAgent legitimately share a namespace/name, stripping
// the prefix would collide with the Agent row's primary key, so the down
// migration must abort (with a unique violation) rather than merge or lose rows.
func TestMigration000007_DownFailsOnSharedName(t *testing.T) {
	connStr := startTestDB(t)
	ctx := context.Background()
	src := BuiltinSources(false)[0]

	if err := WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Migrate(7)
	}); err != nil {
		t.Fatalf("migrate to version 7: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	for _, s := range []struct{ id, typ string }{
		{"default__NS__dup", "Declarative"},
		{"sandboxagents__NS__default__NS__dup", "SandboxAgent"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent (id, type) VALUES ($1, $2)`, s.id, s.typ); err != nil {
			t.Fatalf("seed agent %s: %v", s.id, err)
		}
	}

	err = WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Steps(-1)
	})
	if err == nil {
		t.Fatal("down migration succeeded despite a shared-name collision; expected a unique violation")
	}
}
