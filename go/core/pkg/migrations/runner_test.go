package migrations

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var twoMigrationFS = fstest.MapFS{
	"migrations/000001_create.sql": {Data: migrationSQL(
		"CREATE TABLE migration_test (id bigint PRIMARY KEY);",
		"DROP TABLE IF EXISTS migration_test;",
	)},
	"migrations/000002_alter.sql": {Data: migrationSQL(
		"ALTER TABLE migration_test ADD COLUMN name text;",
		"ALTER TABLE migration_test DROP COLUMN IF EXISTS name;",
	)},
}

func migrationSQL(up, down string) []byte {
	return []byte("-- +goose Up\n" + up + "\n\n-- +goose Down\n" + down + "\n")
}

func testSource(fsys fstest.MapFS) Source {
	return Source{
		Name:          "test",
		TrackingTable: "test_schema_migrations",
		FS:            fsys,
		Dir:           "migrations",
	}
}

func startTestDB(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skip the PostgreSQL test in short mode")
	}
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
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
		t.Fatalf("start PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminate PostgreSQL: %v", err)
		}
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	return dsn
}

func execSQL(t *testing.T, dsn, statement string, args ...any) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), statement, args...); err != nil {
		t.Fatalf("execute SQL: %v", err)
	}
}

func testTableExists(t *testing.T, dsn, table string) bool {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var exists bool
	if err := db.QueryRowContext(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func testExtensionExists(t *testing.T, dsn, extension string) bool {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var exists bool
	if err := db.QueryRowContext(context.Background(), "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = $1)", extension).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func testColumnExists(t *testing.T, dsn, table, column string) bool {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var exists bool
	err = db.QueryRowContext(context.Background(), `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func testVersions(t *testing.T, dsn, table string) []int64 {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), "SELECT version_id FROM "+quoteIdentifier(table)+" WHERE is_applied ORDER BY version_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return versions
}

func TestRunUpAndDown(t *testing.T) {
	dsn := startTestDB(t)
	source := testSource(twoMigrationFS)

	if err := RunUp(context.Background(), dsn, []Source{source}); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	if !testTableExists(t, dsn, "migration_test") {
		t.Fatal("migration_test does not exist")
	}
	if !testColumnExists(t, dsn, "migration_test", "name") {
		t.Fatal("migration_test.name does not exist")
	}
	if got := testVersions(t, dsn, source.TrackingTable); !slices.Equal(got, []int64{0, 1, 2}) {
		t.Fatalf("versions = %v", got)
	}
	if err := VerifyMigrated(context.Background(), dsn, []Source{source}); err != nil {
		t.Fatalf("VerifyMigrated: %v", err)
	}

	if err := WithProvider(context.Background(), dsn, source, func(provider *goose.Provider) error {
		_, err := provider.DownTo(context.Background(), 0)
		return err
	}); err != nil {
		t.Fatalf("DownTo: %v", err)
	}
	if testTableExists(t, dsn, "migration_test") {
		t.Fatal("migration_test still exists")
	}
	if got := testVersions(t, dsn, source.TrackingTable); !slices.Equal(got, []int64{0}) {
		t.Fatalf("versions after down = %v", got)
	}
}

func TestBuiltinMigrationsRoundTrip(t *testing.T) {
	dsn := startTestDB(t)
	sources := BuiltinSources(true)

	if err := RunUp(context.Background(), dsn, sources); err != nil {
		t.Fatalf("initial RunUp: %v", err)
	}
	if err := VerifyMigrated(context.Background(), dsn, sources); err != nil {
		t.Fatalf("initial VerifyMigrated: %v", err)
	}
	// A context may outlive one AgentInstance, so these IDs intentionally differ.
	contextID := "00000000-0000-0000-0000-000000000001"
	instanceID := "00000000-0000-0000-0000-000000000002"
	execSQL(t, dsn, "INSERT INTO a2a_context (id, namespace, user_id) VALUES ($1, 'test', 'user')", contextID)
	execSQL(t, dsn, "INSERT INTO agent_instance (id, namespace, user_id, request_id, state, data, context_id) VALUES ($1, 'test', 'user', 'request', 'READY', $2, $3)", instanceID, []byte{}, contextID)
	execSQL(t, dsn, "INSERT INTO agent_instance_task (context_id, id, state, data) VALUES ($1, 'task', 'TASK_STATE_INPUT_REQUIRED', $2)", contextID, []byte{})
	for _, source := range slices.Backward(sources) {
		if err := WithProvider(context.Background(), dsn, source, func(provider *goose.Provider) error {
			_, err := provider.DownTo(context.Background(), 0)
			return err
		}); err != nil {
			t.Fatalf("down %s: %v", source.Name, err)
		}
	}
	if !testExtensionExists(t, dsn, "vector") {
		t.Fatal("the vector down migration removed the shared extension")
	}
	if err := RunUp(context.Background(), dsn, sources); err != nil {
		t.Fatalf("second RunUp: %v", err)
	}
	if err := VerifyMigrated(context.Background(), dsn, sources); err != nil {
		t.Fatalf("second VerifyMigrated: %v", err)
	}
}

func TestMigrationIsAtomic(t *testing.T) {
	dsn := startTestDB(t)
	source := testSource(fstest.MapFS{
		"migrations/000001_fail.sql": {Data: migrationSQL(
			"CREATE TABLE partial_result (id bigint); ALTER TABLE missing_table ADD COLUMN value text;",
			"DROP TABLE IF EXISTS partial_result;",
		)},
	})

	err := RunUp(context.Background(), dsn, []Source{source})
	if err == nil {
		t.Fatal("RunUp succeeded")
	}
	if testTableExists(t, dsn, "partial_result") {
		t.Fatal("the failed migration left partial_result")
	}
	if got := testVersions(t, dsn, source.TrackingTable); !slices.Equal(got, []int64{0}) {
		t.Fatalf("versions = %v", got)
	}
}

func TestConcurrentRunUp(t *testing.T) {
	dsn := startTestDB(t)
	source := testSource(twoMigrationFS)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errs <- RunUp(context.Background(), dsn, []Source{source})
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("RunUp: %v", err)
		}
	}
	if err := VerifyMigrated(context.Background(), dsn, []Source{source}); err != nil {
		t.Fatalf("VerifyMigrated: %v", err)
	}
}

func TestRunUpContinuesAfterLaterSourceFailure(t *testing.T) {
	dsn := startTestDB(t)
	first := testSource(fstest.MapFS{
		"first/000001_initial.sql": {Data: migrationSQL(
			"CREATE TABLE first_result (id bigint);",
			"DROP TABLE IF EXISTS first_result;",
		)},
	})
	first.Name = "first"
	first.Dir = "first"
	first.TrackingTable = "first_schema_migrations"
	second := testSource(fstest.MapFS{
		"second/000001_initial.sql": {Data: migrationSQL(
			"CREATE TABLE partial_second_result (id bigint); ALTER TABLE missing_table ADD COLUMN value text;",
			"DROP TABLE IF EXISTS partial_second_result;",
		)},
	})
	second.Name = "second"
	second.Dir = "second"
	second.TrackingTable = "second_schema_migrations"

	if err := RunUp(context.Background(), dsn, []Source{first, second}); err == nil {
		t.Fatal("RunUp succeeded")
	}
	if !testTableExists(t, dsn, "first_result") {
		t.Fatal("the completed first source was reversed")
	}
	if testTableExists(t, dsn, "partial_second_result") {
		t.Fatal("the failed second source left a partial result")
	}

	second.FS = fstest.MapFS{
		"second/000001_initial.sql": {Data: migrationSQL(
			"CREATE TABLE second_result (id bigint);",
			"DROP TABLE IF EXISTS second_result;",
		)},
	}
	if err := RunUp(context.Background(), dsn, []Source{first, second}); err != nil {
		t.Fatalf("second RunUp: %v", err)
	}
	if !testTableExists(t, dsn, "second_result") {
		t.Fatal("the second source did not continue")
	}
}

func TestRunUpRejectsOldTrackingTable(t *testing.T) {
	dsn := startTestDB(t)
	source := testSource(fstest.MapFS{
		"migrations/000001_initial.sql": {Data: migrationSQL(
			"CREATE TABLE baseline_ran (id bigint);",
			"DROP TABLE IF EXISTS baseline_ran;",
		)},
	})
	execSQL(t, dsn, "CREATE TABLE "+quoteIdentifier(source.TrackingTable)+" (version bigint PRIMARY KEY, dirty boolean NOT NULL)")
	execSQL(t, dsn, "INSERT INTO "+quoteIdentifier(source.TrackingTable)+" (version, dirty) VALUES (19, false)")

	if err := RunUp(context.Background(), dsn, []Source{source}); err == nil || !strings.Contains(err.Error(), "new PostgreSQL database") {
		t.Fatalf("RunUp error = %v", err)
	}
	if testTableExists(t, dsn, "baseline_ran") {
		t.Fatal("the baseline ran against an old tracking table")
	}
}

func TestRunUpRejectsApplicationTableWithoutTrackingTable(t *testing.T) {
	dsn := startTestDB(t)
	source := testSource(fstest.MapFS{
		"migrations/000001_initial.sql": {Data: migrationSQL(
			"CREATE TABLE baseline_ran (id bigint);",
			"DROP TABLE baseline_ran;",
		)},
	})
	source.OwnedTables = []string{"existing_app"}
	execSQL(t, dsn, "CREATE TABLE existing_app (id bigint)")

	err := RunUp(context.Background(), dsn, []Source{source})
	if err == nil || !strings.Contains(err.Error(), "without a Goose migration table") || !strings.Contains(err.Error(), "new PostgreSQL database") {
		t.Fatalf("RunUp error = %v", err)
	}
	if testTableExists(t, dsn, "baseline_ran") || testTableExists(t, dsn, source.TrackingTable) {
		t.Fatal("the baseline started before the application-table preflight failed")
	}
}

func TestPrechecksRunBeforeMigrations(t *testing.T) {
	dsn := startTestDB(t)
	first := testSource(twoMigrationFS)
	second := testSource(fstest.MapFS{
		"other/000001_initial.sql": {Data: migrationSQL("SELECT 1;", "SELECT 1;")},
	})
	second.Name = "other"
	second.Dir = "other"
	second.TrackingTable = "other_schema_migrations"
	second.PreCheck = func(string) error { return errors.New("blocked") }

	err := RunUp(context.Background(), dsn, []Source{first, second})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("RunUp error = %v", err)
	}
	if testTableExists(t, dsn, first.TrackingTable) || testTableExists(t, dsn, "migration_test") {
		t.Fatal("the first source changed the database before preflight failed")
	}
}

func TestRunUpAllowsDatabaseAhead(t *testing.T) {
	dsn := startTestDB(t)
	source := testSource(fstest.MapFS{
		"migrations/000001_initial.sql": {Data: migrationSQL("SELECT 1;", "SELECT 1;")},
	})
	if err := RunUp(context.Background(), dsn, []Source{source}); err != nil {
		t.Fatal(err)
	}
	execSQL(t, dsn, "INSERT INTO "+quoteIdentifier(source.TrackingTable)+" (version_id, is_applied) VALUES (2, true)")
	if err := RunUp(context.Background(), dsn, []Source{source}); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	if err := VerifyMigrated(context.Background(), dsn, []Source{source}); err != nil {
		t.Fatalf("VerifyMigrated: %v", err)
	}
}

func TestMigrationFileValidation(t *testing.T) {
	tests := []struct {
		name string
		file string
		body string
		want string
	}{
		{name: "valid", body: "-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;"},
		{name: "missing up", body: "-- +goose Down\nSELECT 1;", want: "no Goose Up"},
		{name: "missing down", body: "-- +goose Up\nSELECT 1;", want: "no Goose Down"},
		{name: "no transaction", body: "-- +goose NO TRANSACTION\n-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;", want: "disables transactions"},
		{name: "no transaction with extra spacing", body: "-- +goose   NO TRANSACTION\n-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;", want: "disables transactions"},
		{name: "down text in SQL", body: "-- +goose Up\nSELECT '-- +goose Down';", want: "no Goose Down"},
		{name: "legacy up file", file: "000001_test.up.sql", body: "-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;", want: "invalid migration file"},
		{name: "legacy down file", file: "000001_test.down.sql", body: "-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;", want: "invalid migration file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := tt.file
			if file == "" {
				file = "000001_test.sql"
			}
			source := testSource(fstest.MapFS{
				"migrations/" + file: {Data: []byte(tt.body)},
			})
			_, err := migrationVersions(source)
			if tt.want == "" && err != nil {
				t.Fatalf("migrationVersions: %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("migrationVersions error = %v", err)
			}
		})
	}
}

func TestMigrationVersionsFromRoot(t *testing.T) {
	source := testSource(fstest.MapFS{
		"000001_test.sql": {Data: migrationSQL("SELECT 1;", "SELECT 1;")},
	})
	source.Dir = "."
	if _, err := migrationVersions(source); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSources(t *testing.T) {
	valid := testSource(twoMigrationFS)
	if err := validateSources([]Source{valid}); err != nil {
		t.Fatalf("validateSources: %v", err)
	}

	tests := []Source{
		{Name: "", TrackingTable: valid.TrackingTable, FS: valid.FS, Dir: valid.Dir},
		{Name: "test", TrackingTable: "Bad-Table", FS: valid.FS, Dir: valid.Dir},
		{Name: "test", Schema: "Bad-Schema", TrackingTable: valid.TrackingTable, FS: valid.FS, Dir: valid.Dir},
	}
	for _, source := range tests {
		if err := validateSources([]Source{source}); err == nil {
			t.Fatalf("validateSources(%+v) succeeded", source)
		}
	}

	other := valid
	other.Name = "other"
	if err := validateSources([]Source{valid, other}); err == nil {
		t.Fatal("validateSources accepted a collision")
	}
}

func TestBuiltinTrackingTables(t *testing.T) {
	sources := BuiltinSources(true)
	if len(sources) != 2 {
		t.Fatalf("sources = %d", len(sources))
	}
	if sources[0].TrackingTable != coreTrackingTable {
		t.Fatalf("core source = %+v", sources[0])
	}
	if sources[1].TrackingTable != vectorTrackingTable {
		t.Fatalf("vector source = %+v", sources[1])
	}
}

func TestWithSearchPath(t *testing.T) {
	got, err := withSearchPath("postgres://u:p@host/db?sslmode=disable", "tenant_1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "search_path=tenant_1") || !strings.Contains(got, "sslmode=disable") {
		t.Fatalf("URL = %q", got)
	}
	if _, err := withSearchPath("mysql://host/db", "tenant_1"); err == nil {
		t.Fatal("withSearchPath accepted MySQL")
	}
}

func TestAdvisoryLockIDIsStable(t *testing.T) {
	if got := advisoryLockID("kagent_test", "public", "schema_migrations"); got != 4022843769 {
		t.Fatalf("lock ID = %d", got)
	}
}

func TestEmptySources(t *testing.T) {
	if err := RunUp(context.Background(), "postgres://unused", nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyMigrated(context.Background(), "postgres://unused", nil); err != nil {
		t.Fatal(err)
	}
}
