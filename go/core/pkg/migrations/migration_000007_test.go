package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

// realCoreSource is the embedded core track, as BuiltinSources builds it.
func realCoreSource() Source {
	return Source{Name: "core", TrackingTable: "schema_migrations", FS: FS, Dir: "core"}
}

// migrateCoreTo moves the core track to the given version (up or down).
func migrateCoreTo(t *testing.T, connStr string, version uint) {
	t.Helper()
	err := WithMigrator(context.Background(), connStr, realCoreSource(), func(mg *migrate.Migrate) error {
		return mg.Migrate(version)
	})
	if err != nil {
		t.Fatalf("migrate core to %d: %v", version, err)
	}
}

// TestMigration000007TaskOwnerBackfill verifies the task.user_id backfill:
// ownership is assigned only when a task's session id maps to exactly one
// user across its whole history, deleted sessions included. Anything
// ambiguous or unresolvable stays NULL.
func TestMigration000007TaskOwnerBackfill(t *testing.T) {
	connStr := startTestDB(t)
	migrateCoreTo(t, connStr, 6)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}

	// session's key is (id, user_id), so one session id can have several
	// owners over time; deleted_at marks soft deletion.
	exec(`INSERT INTO session (id, user_id, created_at, updated_at) VALUES ('s-sole', 'alice', NOW(), NOW())`)
	exec(`INSERT INTO session (id, user_id, created_at, updated_at, deleted_at) VALUES ('s-gone', 'alice', NOW(), NOW(), NOW())`)
	exec(`INSERT INTO session (id, user_id, created_at, updated_at, deleted_at) VALUES ('s-shared', 'alice', NOW(), NOW(), NOW())`)
	exec(`INSERT INTO session (id, user_id, created_at, updated_at) VALUES ('s-shared', 'bob', NOW(), NOW())`)

	for _, task := range [][2]string{
		{"t-sole", "s-sole"},
		{"t-gone", "s-gone"},
		{"t-shared", "s-shared"},
		{"t-orphan", "s-missing"},
	} {
		exec(`INSERT INTO task (id, data, session_id, created_at, updated_at) VALUES ($1, '{}', $2, NOW(), NOW())`, task[0], task[1])
	}

	migrateCoreTo(t, connStr, 7)

	alice := "alice"
	tests := []struct {
		task string
		want *string // nil means the task must stay NULL-owned
	}{
		{task: "t-sole", want: &alice}, // session id maps to exactly one user
		{task: "t-gone", want: &alice}, // sole historical owner, session deleted
		{task: "t-shared", want: nil},  // two users across history: ambiguous
		{task: "t-orphan", want: nil},  // no such session
	}
	for _, tt := range tests {
		var got *string
		if err := db.QueryRowContext(ctx, `SELECT user_id FROM task WHERE id = $1`, tt.task).Scan(&got); err != nil {
			t.Fatalf("read owner of %s: %v", tt.task, err)
		}
		switch {
		case tt.want == nil && got != nil:
			t.Errorf("%s: user_id = %q, want NULL", tt.task, *got)
		case tt.want != nil && (got == nil || *got != *tt.want):
			t.Errorf("%s: user_id = %v, want %q", tt.task, got, *tt.want)
		}
	}

	// The down migration drops the column again.
	migrateCoreTo(t, connStr, 6)
	var hasColumn bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'task' AND column_name = 'user_id')`).
		Scan(&hasColumn); err != nil {
		t.Fatalf("check column: %v", err)
	}
	if hasColumn {
		t.Error("down migration should drop task.user_id")
	}
}
