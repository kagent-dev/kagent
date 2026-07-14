package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

// TestMigration000007_KindQualifiedIDs seeds pre-000007 rows (bare ids for all
// kinds, sessions pointing at them), applies the migration, and asserts only
// SandboxAgent/AgentHarness rows and their sessions are rewritten. The down
// migration deletes the experimental rows and their sessions outright (no rollback
// guarantee) and leaves Agent rows byte-identical.
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
		{"default__NS__foo", "Declarative"},       // plain Agent — untouched by up and down
		{"default__NS__box", "SandboxAgent"},      // rewritten by up, deleted by down
		{"default__NS__claw", "AgentHarness"},     // rewritten by up, deleted by down
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
		if _, err := db.ExecContext(ctx,
			`INSERT INTO event (id, user_id, session_id, data) VALUES ($1, 'u1', $2, '{}')`, "event-"+s.id, "session-"+s.id); err != nil {
			t.Fatalf("seed event for %s: %v", s.id, err)
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

	// Agents (including the prefix-named-namespace one) keep rows, sessions, events.
	assertIDs(map[string]string{
		"session-default__NS__foo":       "default__NS__foo",
		"session-sandboxagents__NS__foo": "sandboxagents__NS__foo",
	})

	// Experimental rows and their sessions/events are gone.
	for _, name := range []string{"box", "claw"} {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM agent WHERE id LIKE '%default__NS__'||$1`, name).Scan(&n); err != nil {
			t.Fatalf("count agent rows for %s: %v", name, err)
		}
		if n != 0 {
			t.Errorf("expected no agent rows for %s after down, got %d", name, n)
		}
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM session WHERE id = $1`, "session-default__NS__"+name).Scan(&n); err != nil {
			t.Fatalf("count sessions for %s: %v", name, err)
		}
		if n != 0 {
			t.Errorf("expected session for %s deleted after down, got %d", name, n)
		}
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM event WHERE session_id = $1`, "session-default__NS__"+name).Scan(&n); err != nil {
			t.Fatalf("count events for %s: %v", name, err)
		}
		if n != 0 {
			t.Errorf("expected events for %s deleted after down, got %d", name, n)
		}
	}
}

// TestMigration000007_ReRunnableAfterRollbackWindow pins the operational rollback
// scenario: after 000007, a 0.9.x binary run against the database recreates BARE
// rows for live SandboxAgent resources (duplicates of the qualified rows). The down
// migration must remove the qualified side so that re-upgrading (up migration
// qualifying the bare rows) does not abort on a primary-key collision.
func TestMigration000007_ReRunnableAfterRollbackWindow(t *testing.T) {
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
		{"sandboxagents__NS__default__NS__box", "SandboxAgent"}, // migrated row, pre-rollback history
		{"default__NS__box", "SandboxAgent"},                    // bare row recreated by 0.9.x during the window
		{"default__NS__dup", "Declarative"},                     // Agent sharing a name with a qualified row
		{"sandboxagents__NS__default__NS__dup", "SandboxAgent"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent (id, type) VALUES ($1, $2)`, s.id, s.typ); err != nil {
			t.Fatalf("seed agent %s: %v", s.id, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO session (id, user_id, agent_id) VALUES ('pre-rollback-session', 'u1', 'sandboxagents__NS__default__NS__box')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Steps(-1)
	}); err != nil {
		t.Fatalf("down migration: %v", err)
	}

	// The qualified rows and their sessions are gone; the bare 0.9.x row and the
	// Agent survive untouched.
	for id, want := range map[string]int{
		"sandboxagents__NS__default__NS__box": 0,
		"sandboxagents__NS__default__NS__dup": 0,
		"default__NS__box":                    1,
		"default__NS__dup":                    1,
	} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM agent WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", id, err)
		}
		if n != want {
			t.Errorf("agent %s: got %d rows, want %d", id, n, want)
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM session WHERE id = 'pre-rollback-session'`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("qualified row's session must be deleted by down, got %d", n)
	}

	// Re-upgrade succeeds and qualifies the surviving bare sandbox row.
	if err := WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Migrate(7)
	}); err != nil {
		t.Fatalf("re-apply 000007 after rollback window: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM agent WHERE id = 'sandboxagents__NS__default__NS__box'`).Scan(&n); err != nil {
		t.Fatalf("count re-qualified row: %v", err)
	}
	if n != 1 {
		t.Errorf("re-applied up migration must qualify the bare sandbox row, got %d", n)
	}
}
