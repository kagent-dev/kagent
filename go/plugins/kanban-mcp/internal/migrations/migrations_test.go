package migrations

import (
	"context"
	"database/sql"
	"os/exec"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPostgres starts a Postgres container and returns its connection string,
// registering termination with t.Cleanup. Tests are skipped when Docker is not
// available so they remain runnable in environments without a container runtime.
// This is a thin local copy of go/core/internal/dbtest (which cannot be imported
// across the internal/ boundary).
func startPostgres(ctx context.Context, t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping container test")
	}

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("kanban_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("kanban"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	// The container's mapped port may not be reachable on the host the instant
	// the readiness log fires (host port-forwarding can lag), so wait for the
	// connection to actually succeed before tests open it directly.
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("open db for readiness wait: %v", err)
	}
	defer db.Close()
	if err := waitForDB(db); err != nil {
		t.Fatalf("waiting for database: %v", err)
	}
	return connStr
}

func TestRunUp(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(ctx, t)

	if err := RunUp(url); err != nil {
		t.Fatalf("RunUp() error = %v", err)
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"kanban.task", "kanban.attachment", "kanban.board", "kanban.subtask"} {
		var reg *string
		if err := db.QueryRow("SELECT to_regclass($1)", table).Scan(&reg); err != nil {
			t.Fatalf("to_regclass(%s): %v", table, err)
		}
		if reg == nil {
			t.Errorf("table %s does not exist after RunUp", table)
		}
	}

	// The built-in default board must be seeded with the 7 workflow columns.
	var defaultColCount int
	if err := db.QueryRow(
		"SELECT array_length(columns, 1) FROM kanban.board WHERE key = 'default'",
	).Scan(&defaultColCount); err != nil {
		t.Fatalf("reading default board: %v", err)
	}
	if defaultColCount != 7 {
		t.Errorf("default board columns = %d, want 7", defaultColCount)
	}

	var migCount int
	if err := db.QueryRow("SELECT count(*) FROM kanban_schema_migrations").Scan(&migCount); err != nil {
		t.Fatalf("count kanban_schema_migrations: %v", err)
	}
	if migCount == 0 {
		t.Errorf("expected at least one row in kanban_schema_migrations, got 0")
	}

	// Running RunUp again must be a no-op (ErrNoChange handled as success).
	if err := RunUp(url); err != nil {
		t.Fatalf("RunUp() second call error = %v", err)
	}
}

func TestRunUp_Coexist(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(ctx, t)

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Simulate the kagent core schema owning a top-level public.task table.
	if _, err := db.Exec("CREATE TABLE public.task (id BIGINT PRIMARY KEY)"); err != nil {
		t.Fatalf("create public.task: %v", err)
	}

	if err := RunUp(url); err != nil {
		t.Fatalf("RunUp() error = %v", err)
	}

	for _, table := range []string{"public.task", "kanban.task"} {
		var reg *string
		if err := db.QueryRow("SELECT to_regclass($1)", table).Scan(&reg); err != nil {
			t.Fatalf("to_regclass(%s): %v", table, err)
		}
		if reg == nil {
			t.Errorf("expected %s to exist, got NULL", table)
		}
	}

	// public.task must remain untouched (no kanban columns leaked into it).
	var colCount int
	if err := db.QueryRow(
		"SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='task'",
	).Scan(&colCount); err != nil {
		t.Fatalf("count public.task columns: %v", err)
	}
	if colCount != 1 {
		t.Errorf("public.task should have 1 column (id), got %d", colCount)
	}
}
