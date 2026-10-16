package migrations

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
)

func baselineTestSource(schema string) Source {
	fsys := maps.Clone(twoMigrationFS)
	fsys["migrations/000001_create.sql"] = &fstest.MapFile{Data: migrationSQL(
		"CREATE TABLE migration_test (id bigint PRIMARY KEY); INSERT INTO migration_test VALUES (1);",
		"DROP TABLE migration_test;",
	)}
	fsys["migrations/000002_alter.sql"] = &fstest.MapFile{Data: migrationSQL(
		"ALTER TABLE migration_test ADD COLUMN name text NOT NULL DEFAULT 'history';",
		"ALTER TABLE migration_test DROP COLUMN name;",
	)}
	fsys["migrations/000003_tail.sql"] = &fstest.MapFile{Data: migrationSQL(
		"ALTER TABLE migration_test ADD COLUMN active boolean NOT NULL DEFAULT true;",
		"ALTER TABLE migration_test DROP COLUMN active;",
	)}
	fsys["baseline/000002_initial.sql"] = &fstest.MapFile{Data: migrationSQL(
		"CREATE TABLE migration_test (id bigint PRIMARY KEY, name text NOT NULL DEFAULT 'history'); INSERT INTO migration_test VALUES (1, 'baseline');",
		"DROP TABLE migration_test;",
	)}
	source := testSource(fsys)
	source.Schema = schema
	source.BaselineDir = "baseline"
	return source
}

func baselineTestDSN(t *testing.T, dsn string, source Source) string {
	t.Helper()
	scoped, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := scoped.Query()
	query.Set("search_path", source.Schema)
	scoped.RawQuery = query.Encode()
	return scoped.String()
}

func requireMigrationName(t *testing.T, dsn, want string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRowContext(t.Context(), "SELECT name FROM migration_test WHERE id = 1").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("migration row = %q, want %q", got, want)
	}
}

func TestRunUpBaseline(t *testing.T) {
	dsn, config := startTestDB(t)

	t.Run("fresh install, retry, and historical down", func(t *testing.T) {
		source := baselineTestSource("baseline_fresh")
		scoped := baselineTestDSN(t, dsn, source)
		for range 2 {
			if err := RunUp(t.Context(), config, []Source{source}); err != nil {
				t.Fatal(err)
			}
		}
		requireMigrationName(t, scoped, "baseline")
		if !testColumnExists(t, scoped, "migration_test", "active") {
			t.Fatal("the migration after the baseline did not run")
		}
		if got := testVersions(t, scoped, source.TrackingTable); !slices.Equal(got, []int64{0, 1, 2, 3}) {
			t.Fatalf("versions = %v", got)
		}
		if err := VerifyMigrated(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		if err := WithProvider(t.Context(), config, source, func(provider *goose.Provider) error {
			_, err := provider.DownTo(t.Context(), 0)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if testTableExists(t, scoped, "migration_test") {
			t.Fatal("historical Down left the baseline table behind")
		}
		if err := RunUp(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		requireMigrationName(t, scoped, "baseline")
	})

	t.Run("existing data uses retained history", func(t *testing.T) {
		source := baselineTestSource("baseline_upgrade")
		scoped := baselineTestDSN(t, dsn, source)
		if err := WithProvider(t.Context(), config, source, func(provider *goose.Provider) error {
			_, err := provider.UpTo(t.Context(), 1)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		execSQL(t, scoped, "INSERT INTO migration_test VALUES (77)")
		if err := RunUp(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		requireMigrationName(t, scoped, "history")
		execSQL(t, scoped, "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM migration_test WHERE id = 77 AND active) THEN RAISE EXCEPTION 'existing row lost'; END IF; END $$")
	})

	t.Run("version zero remains eligible", func(t *testing.T) {
		source := baselineTestSource("baseline_zero")
		if err := WithProvider(t.Context(), config, source, func(provider *goose.Provider) error {
			_, err := provider.GetDBVersion(t.Context())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := RunUp(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		requireMigrationName(t, baselineTestDSN(t, dsn, source), "baseline")
	})

	t.Run("failed baseline rolls back and retries", func(t *testing.T) {
		source := baselineTestSource("baseline_failure")
		fsys := source.FS.(fstest.MapFS)
		good := fsys["baseline/000002_initial.sql"]
		fsys["baseline/000002_initial.sql"] = &fstest.MapFile{Data: migrationSQL(
			"CREATE TABLE migration_test (id bigint PRIMARY KEY); SELECT 1 / 0;", "DROP TABLE migration_test;",
		)}
		if err := RunUp(t.Context(), config, []Source{source}); err == nil {
			t.Fatal("RunUp accepted the failing baseline")
		}
		scoped := baselineTestDSN(t, dsn, source)
		if testTableExists(t, scoped, "migration_test") {
			t.Fatal("failed baseline committed its DDL")
		}
		if got := testVersions(t, scoped, source.TrackingTable); !slices.Equal(got, []int64{0}) {
			t.Fatalf("failed baseline recorded versions %v", got)
		}
		fsys["baseline/000002_initial.sql"] = good
		if err := RunUp(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		requireMigrationName(t, scoped, "baseline")
	})

	t.Run("committed baseline survives a failed tail", func(t *testing.T) {
		source := baselineTestSource("baseline_tail_failure")
		fsys := source.FS.(fstest.MapFS)
		good := fsys["migrations/000003_tail.sql"]
		fsys["migrations/000003_tail.sql"] = &fstest.MapFile{Data: migrationSQL("SELECT 1 / 0;", "SELECT 1;")}
		if err := RunUp(t.Context(), config, []Source{source}); err == nil {
			t.Fatal("RunUp accepted the failing tail")
		}
		scoped := baselineTestDSN(t, dsn, source)
		requireMigrationName(t, scoped, "baseline")
		if got := testVersions(t, scoped, source.TrackingTable); !slices.Equal(got, []int64{0, 1, 2}) {
			t.Fatalf("versions after failed tail = %v", got)
		}
		fsys["migrations/000003_tail.sql"] = good
		if err := RunUp(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		requireMigrationName(t, scoped, "baseline")
	})

	t.Run("ledger failure rolls back DDL and covered versions", func(t *testing.T) {
		source := baselineTestSource("baseline_ledger_failure")
		scoped := baselineTestDSN(t, dsn, source)
		if err := WithProvider(t.Context(), config, source, func(provider *goose.Provider) error {
			_, err := provider.GetDBVersion(t.Context())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		execSQL(t, scoped, "ALTER TABLE test_schema_migrations ADD CONSTRAINT reject_baseline CHECK (version_id <> 2)")
		if err := RunUp(t.Context(), config, []Source{source}); err == nil {
			t.Fatal("RunUp accepted an unrecorded baseline")
		}
		if testTableExists(t, scoped, "migration_test") {
			t.Fatal("ledger failure committed the baseline DDL")
		}
		if got := testVersions(t, scoped, source.TrackingTable); !slices.Equal(got, []int64{0}) {
			t.Fatalf("ledger failure committed versions %v", got)
		}
		execSQL(t, scoped, "ALTER TABLE test_schema_migrations DROP CONSTRAINT reject_baseline")
		if err := RunUp(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		requireMigrationName(t, scoped, "baseline")
	})

	t.Run("concurrent starts serialize the baseline", func(t *testing.T) {
		source := baselineTestSource("baseline_concurrent")
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for range 4 {
			wg.Go(func() { errs <- RunUp(t.Context(), config, []Source{source}) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		requireMigrationName(t, baselineTestDSN(t, dsn, source), "baseline")
	})

	t.Run("verification stays read only", func(t *testing.T) {
		source := baselineTestSource("public")
		if err := VerifyMigrated(t.Context(), config, []Source{source}); err == nil {
			t.Fatal("VerifyMigrated accepted the unmigrated source")
		}
		if testTableExists(t, dsn, source.TrackingTable) || testTableExists(t, dsn, "migration_test") {
			t.Fatal("verification wrote migration state")
		}
	})

	t.Run("baseline uses the configured schema and role", func(t *testing.T) {
		source := baselineTestSource("baseline_owned")
		execSQL(t, dsn, "CREATE ROLE baseline_app NOLOGIN; CREATE SCHEMA baseline_owned AUTHORIZATION baseline_app")
		if err := RunUp(t.Context(), testConfig(t, dsn, "baseline_app"), []Source{source}); err != nil {
			t.Fatal(err)
		}
		scoped := baselineTestDSN(t, dsn, source)
		requireMigrationName(t, scoped, "baseline")
		execSQL(t, scoped, "DO $$ BEGIN IF (SELECT tableowner FROM pg_tables WHERE schemaname = 'baseline_owned' AND tablename = 'migration_test') <> 'baseline_app' THEN RAISE EXCEPTION 'wrong owner'; END IF; END $$")
		if err := VerifyMigrated(t.Context(), testConfig(t, dsn, "baseline_app"), []Source{source}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invalid baselines fail before SQL", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			files   fstest.MapFS
			remove  bool
			want    string
			wantErr error
		}{
			{name: "missing", remove: true, wantErr: fs.ErrNotExist},
			{name: "multiple", files: fstest.MapFS{"baseline/000001_other.sql": {Data: migrationSQL("SELECT 1;", "SELECT 1;")}}, want: "exactly one"},
			{name: "unknown_cutoff", remove: true, files: fstest.MapFS{"baseline/000004_initial.sql": {Data: migrationSQL("SELECT 1;", "SELECT 1;")}}, want: "not in migration history"},
			{name: "nontransactional", files: fstest.MapFS{"baseline/000002_initial.sql": {Data: append([]byte("-- +goose NO TRANSACTION\n"), migrationSQL("CREATE TABLE forbidden (id int);", "DROP TABLE forbidden;")...)}}, want: "disables transactions"},
			{name: "no_down", files: fstest.MapFS{"baseline/000002_initial.sql": {Data: []byte("-- +goose Up\nCREATE TABLE forbidden (id int);")}}, want: "no Goose Down"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				source := baselineTestSource("baseline_invalid_" + tc.name)
				fsys := source.FS.(fstest.MapFS)
				if tc.remove {
					delete(fsys, "baseline/000002_initial.sql")
				}
				maps.Copy(fsys, tc.files)
				err := RunUp(t.Context(), config, []Source{source})
				if err == nil || !strings.Contains(err.Error(), tc.want) || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
					t.Fatalf("RunUp = %v, want %q", err, tc.want)
				}
				scoped := baselineTestDSN(t, dsn, source)
				if testTableExists(t, scoped, "forbidden") || testTableExists(t, scoped, source.TrackingTable) {
					t.Fatal("invalid baseline wrote schema or ledger state")
				}
			})
		}
	})

	t.Run("canceled baseline rolls back", func(t *testing.T) {
		source := baselineTestSource("baseline_canceled")
		fsys := source.FS.(fstest.MapFS)
		good := fsys["baseline/000002_initial.sql"]
		fsys["baseline/000002_initial.sql"] = &fstest.MapFile{Data: migrationSQL(
			"CREATE TABLE migration_test (id bigint PRIMARY KEY); SELECT pg_sleep(30);", "DROP TABLE migration_test;",
		)}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- RunUp(ctx, config, []Source{source}) }()
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		deadline := time.After(5 * time.Second)
		for {
			var sleeping bool
			if err := db.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event = 'PgSleep')").Scan(&sleeping); err != nil {
				t.Fatal(err)
			}
			if sleeping {
				break
			}
			select {
			case err := <-done:
				t.Fatalf("baseline returned before cancellation: %v", err)
			case <-deadline:
				t.Fatal("baseline never reached its blocking statement")
			case <-time.After(10 * time.Millisecond):
			}
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled RunUp = %v", err)
		}
		scoped := baselineTestDSN(t, dsn, source)
		if testTableExists(t, scoped, "migration_test") {
			t.Fatal("canceled baseline committed its DDL")
		}
		fsys["baseline/000002_initial.sql"] = good
		if err := RunUp(t.Context(), config, []Source{source}); err != nil {
			t.Fatal(err)
		}
		requireMigrationName(t, scoped, "baseline")
	})
}
