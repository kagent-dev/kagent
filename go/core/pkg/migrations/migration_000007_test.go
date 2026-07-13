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

// TestMigration000007_DownDedupsRollbackGhosts pins the ghost-dedup behavior: a
// bare row of the SAME kind as a qualified row for the same resource (created by a
// 0.9.x binary during a rollback window) is deleted by the down migration, the
// qualified row is stripped in its place, and sessions from both sides of the
// window converge on the surviving bare id.
func TestMigration000007_DownDedupsRollbackGhosts(t *testing.T) {
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
		{"default__NS__box", "SandboxAgent"},                    // ghost recreated by 0.9.x during rollback
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent (id, type) VALUES ($1, $2)`, s.id, s.typ); err != nil {
			t.Fatalf("seed agent %s: %v", s.id, err)
		}
	}
	// A soft-deleted qualified row must lose to a live bare row of the same name
	// instead of hijacking it: the dedup deletes the soft-deleted side.
	for _, stmt := range []string{
		`INSERT INTO agent (id, type, deleted_at) VALUES ('sandboxagents__NS__default__NS__gone', 'SandboxAgent', NOW())`,
		`INSERT INTO agent (id, type) VALUES ('default__NS__gone', 'SandboxAgent')`,
		// And the converse across kinds: a soft-deleted bare Agent must lose to a
		// live qualified SandboxAgent taking its id (soft-delete Agent, then create
		// a same-named SandboxAgent — the workflow this feature enables).
		`INSERT INTO agent (id, type, deleted_at) VALUES ('default__NS__reborn', 'Declarative', NOW())`,
		`INSERT INTO agent (id, type) VALUES ('sandboxagents__NS__default__NS__reborn', 'SandboxAgent')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed soft-deleted pair: %v", err)
		}
	}
	for _, s := range []struct{ id, agentID string }{
		{"pre-rollback-session", "sandboxagents__NS__default__NS__box"},
		{"window-session", "default__NS__box"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO session (id, user_id, agent_id) VALUES ($1, 'u1', $2)`, s.id, s.agentID); err != nil {
			t.Fatalf("seed session %s: %v", s.id, err)
		}
	}

	if err := WithMigrator(ctx, connStr, src, func(mg *migrate.Migrate) error {
		return mg.Steps(-1)
	}); err != nil {
		t.Fatalf("down migration failed despite the bare row being a same-kind ghost: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM agent WHERE id = 'default__NS__box'`).Scan(&count); err != nil {
		t.Fatalf("count agent rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly one surviving row for the resource, got %d", count)
	}

	var liveGone int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM agent WHERE id = 'default__NS__gone' AND deleted_at IS NULL`).Scan(&liveGone); err != nil {
		t.Fatalf("count live rows for soft-deleted pair: %v", err)
	}
	if liveGone != 1 {
		t.Errorf("live bare row must survive a collision with a soft-deleted qualified row, got %d live rows", liveGone)
	}

	var reborn struct {
		typ  string
		live int
	}
	if err := db.QueryRowContext(ctx,
		`SELECT type, count(*) FROM agent WHERE id = 'default__NS__reborn' GROUP BY type`).Scan(&reborn.typ, &reborn.live); err != nil {
		t.Fatalf("read reborn row: %v", err)
	}
	if reborn.typ != "SandboxAgent" || reborn.live != 1 {
		t.Errorf("live qualified SandboxAgent must win over the soft-deleted bare Agent: got type=%s count=%d", reborn.typ, reborn.live)
	}
	for _, sessionID := range []string{"pre-rollback-session", "window-session"} {
		var agentID string
		if err := db.QueryRowContext(ctx, `SELECT agent_id FROM session WHERE id = $1`, sessionID).Scan(&agentID); err != nil {
			t.Fatalf("read session %s: %v", sessionID, err)
		}
		if agentID != "default__NS__box" {
			t.Errorf("session %s agent_id = %q, want %q", sessionID, agentID, "default__NS__box")
		}
	}
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
