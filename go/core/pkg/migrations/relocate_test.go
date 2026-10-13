package migrations

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
)

// migratePublicLayout migrates the database the way releases before the
// configurable schemas did, into public with pgvector there, and stores a row
// in a core table, in the vector table and in an identity sequence.
func migratePublicLayout(t *testing.T, dsn string) {
	t.Helper()
	if err := RunUp(t.Context(), dsn, BuiltinSourcesInSchema(true, legacySchema, legacySchema)); err != nil {
		t.Fatalf("RunUp in public: %v", err)
	}
	execSQL(t, dsn, `INSERT INTO public.toolserver (name, group_kind) VALUES ('kept', 'RemoteMCPServer')`)
	execSQL(t, dsn, `INSERT INTO public.memory (agent_name, content) VALUES ('agent', 'remembered')`)
	execSQL(t, dsn, `SELECT nextval(pg_get_serial_sequence('public.session_task', 'position'))`)
}

func queryString(t *testing.T, dsn, query string, args ...any) string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}

func relationsIn(t *testing.T, dsn, schema string) []string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p', 'v', 'm', 'S') ORDER BY c.relname`, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

func extensionSchema(t *testing.T, dsn string) string {
	t.Helper()
	return queryString(t, dsn, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'vector'`)
}

func TestRunUpMovesThePublicLayout(t *testing.T) {
	dsn := startTestDB(t)
	migratePublicLayout(t, dsn)
	execSQL(t, dsn, `CREATE TABLE public.unrelated (id int)`)
	sources := BuiltinSources(true)

	if err := RunUp(t.Context(), dsn, sources); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	if err := VerifyMigrated(t.Context(), dsn, sources); err != nil {
		t.Fatalf("VerifyMigrated: %v", err)
	}
	if got := relationsIn(t, dsn, legacySchema); !slices.Equal(got, []string{"unrelated"}) {
		t.Fatalf("public holds %v after the move, want only the unrelated table", got)
	}
	if got := extensionSchema(t, dsn); got != "extensions" {
		t.Fatalf("pgvector is in %q, want extensions", got)
	}
	if got := queryString(t, dsn, `SELECT name FROM kagent.toolserver`); got != "kept" {
		t.Fatalf("toolserver row = %q", got)
	}
	if got := queryString(t, dsn, `SELECT content FROM kagent.memory`); got != "remembered" {
		t.Fatalf("memory row = %q", got)
	}
	if got := queryString(t, dsn, `SELECT pg_get_serial_sequence('kagent.session_task', 'position')`); got != "kagent.session_task_position_seq" {
		t.Fatalf("the identity sequence is %s, want it in kagent", got)
	}
	if got := queryString(t, dsn, `SELECT nextval('kagent.session_task_position_seq')::text`); got != "2" {
		t.Fatalf("the identity sequence continued at %s, want 2", got)
	}
	if got := queryString(t, dsn, `SELECT max(version_id)::text FROM kagent.schema_migrations`); got == "0" {
		t.Fatal("the core tracking table lost its versions")
	}

	// A second start finds the configured layout and changes nothing.
	before := relationsIn(t, dsn, "kagent")
	if err := RunUp(t.Context(), dsn, sources); err != nil {
		t.Fatalf("second RunUp: %v", err)
	}
	if after := relationsIn(t, dsn, "kagent"); !slices.Equal(before, after) {
		t.Fatalf("second RunUp changed kagent from %v to %v", before, after)
	}
}

func TestRunUpMovesThePublicLayoutOnce(t *testing.T) {
	dsn := startTestDB(t)
	migratePublicLayout(t, dsn)
	const runs = 3
	errs := make(chan error, runs)
	start := make(chan struct{})
	for range runs {
		go func() {
			<-start
			errs <- RunUp(context.Background(), dsn, BuiltinSources(true))
		}()
	}
	close(start)
	for range runs {
		if err := <-errs; err != nil {
			t.Fatalf("RunUp: %v", err)
		}
	}
	if got := queryString(t, dsn, `SELECT name FROM kagent.toolserver`); got != "kept" {
		t.Fatalf("toolserver row = %q", got)
	}
}

func TestRunUpLeavesTheTargetLayout(t *testing.T) {
	dsn := startTestDB(t)
	execSQL(t, dsn, `DROP EXTENSION vector; CREATE SCHEMA extensions; CREATE EXTENSION vector WITH SCHEMA extensions`)
	// Tables of another application in public share names with Kagent's.
	execSQL(t, dsn, `CREATE TABLE public.session (id int); CREATE TABLE public.memory (id int)`)
	sources := BuiltinSources(true)
	if err := RunUp(t.Context(), dsn, sources); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	execSQL(t, dsn, `INSERT INTO kagent.toolserver (name, group_kind) VALUES ('kept', 'RemoteMCPServer')`)
	before := relationsIn(t, dsn, "kagent")

	if err := RunUp(t.Context(), dsn, sources); err != nil {
		t.Fatalf("second RunUp: %v", err)
	}
	if after := relationsIn(t, dsn, "kagent"); !slices.Equal(before, after) {
		t.Fatalf("second RunUp changed kagent from %v to %v", before, after)
	}
	if got := relationsIn(t, dsn, legacySchema); !slices.Equal(got, []string{"memory", "session"}) {
		t.Fatalf("public holds %v, want the other application's tables only", got)
	}
	if got := queryString(t, dsn, `SELECT name FROM kagent.toolserver`); got != "kept" {
		t.Fatalf("toolserver row = %q", got)
	}
}

func TestRunUpRefusesAHalfMovedDatabase(t *testing.T) {
	for name, split := range map[string]func(t *testing.T, dsn string){
		"a table moved by hand": func(t *testing.T, dsn string) {
			execSQL(t, dsn, `CREATE SCHEMA kagent; ALTER TABLE public.session SET SCHEMA kagent`)
		},
		"a rolled-back controller's tables beside the moved ones": func(t *testing.T, dsn string) {
			if err := RunUp(t.Context(), dsn, BuiltinSources(true)); err != nil {
				t.Fatalf("RunUp moving the tables: %v", err)
			}
			// A controller from before the move finds public empty.
			if err := RunUp(t.Context(), dsn, BuiltinSourcesInSchema(false, legacySchema, legacySchema)); err != nil {
				t.Fatalf("RunUp in public: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dsn := startTestDB(t)
			migratePublicLayout(t, dsn)
			split(t, dsn)
			before := relationsIn(t, dsn, legacySchema)
			vectorBefore := extensionSchema(t, dsn)
			sources := BuiltinSources(true)

			err := RunUp(t.Context(), dsn, sources)
			if !errors.Is(err, ErrHalfMoved) || !strings.Contains(err.Error(), "refusing to start") {
				t.Fatalf("RunUp error = %v, want %v", err, ErrHalfMoved)
			}
			if err := VerifyMigrated(t.Context(), dsn, sources); !errors.Is(err, ErrHalfMoved) {
				t.Fatalf("VerifyMigrated error = %v, want %v", err, ErrHalfMoved)
			}
			if after := relationsIn(t, dsn, legacySchema); !slices.Equal(before, after) {
				t.Fatalf("the refused RunUp changed public from %v to %v", before, after)
			}
			if got := extensionSchema(t, dsn); got != vectorBefore {
				t.Fatalf("the refused RunUp moved pgvector from %q to %q", vectorBefore, got)
			}
		})
	}
}

func TestVerifyMigratedRefusesThePublicLayout(t *testing.T) {
	dsn := startTestDB(t)
	migratePublicLayout(t, dsn)
	err := VerifyMigrated(t.Context(), dsn, BuiltinSources(true))
	if err == nil || !strings.Contains(err.Error(), "start once with migrations enabled") {
		t.Fatalf("VerifyMigrated error = %v", err)
	}
	if !testTableExists(t, dsn, "public."+coreTrackingTable) {
		t.Fatal("VerifyMigrated moved the tables")
	}
}

// Every relation the built-in migrations leave in their schema, other than the
// identity sequences their tables carry along, is one the move knows.
func TestBuiltinRelationNamesCoverTheMigrations(t *testing.T) {
	dsn := startTestDB(t)
	if err := RunUp(t.Context(), dsn, BuiltinSourcesInSchema(true, "tenant_one", legacySchema)); err != nil {
		t.Fatal(err)
	}
	names, err := builtinRelationNames()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'tenant_one' AND c.relkind IN ('r', 'p', 'v', 'm', 'S')
		AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype IN ('a', 'i'))`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(names, name) {
			t.Errorf("the move does not know %s", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
