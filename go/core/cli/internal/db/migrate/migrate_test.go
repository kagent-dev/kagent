package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// --- fixtures ---

var alphaFS = fstest.MapFS{
	"alpha/000001_create.sql": {Data: migrationFile(
		`CREATE TABLE IF NOT EXISTS cli_alpha (id SERIAL PRIMARY KEY);`,
		`DROP TABLE IF EXISTS cli_alpha;`,
	)},
	"alpha/000002_alter.sql": {Data: migrationFile(
		`ALTER TABLE cli_alpha ADD COLUMN IF NOT EXISTS name TEXT;`,
		`ALTER TABLE cli_alpha DROP COLUMN IF EXISTS name;`,
	)},
}

var betaFS = fstest.MapFS{
	"beta/000001_create.sql": {Data: migrationFile(
		`CREATE TABLE IF NOT EXISTS cli_beta (id SERIAL PRIMARY KEY);`,
		`DROP TABLE IF EXISTS cli_beta;`,
	)},
}

func migrationFile(up, down string) []byte {
	return []byte("-- +goose Up\n" + up + "\n-- +goose Down\n" + down + "\n")
}

func testSources() []migrations.Source {
	return []migrations.Source{
		{Name: "alpha", TrackingTable: "alpha_schema_migrations", FS: alphaFS, Dir: "alpha"},
		{Name: "beta", TrackingTable: "beta_schema_migrations", FS: betaFS, Dir: "beta"},
	}
}

// runCLI executes `db migrate <args>` against a fresh command tree and
// returns combined stdout and the error, plus stderr separately.
func runCLI(t *testing.T, sources []migrations.Source, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewCommand(sources...)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// --- unit tests (no database) ---

func TestNewCommandRejectsBadSources(t *testing.T) {
	tests := []struct {
		name    string
		sources []migrations.Source
	}{
		{name: "invalid name", sources: []migrations.Source{{Name: "Bad Name", TrackingTable: "t", FS: alphaFS, Dir: "alpha"}}},
		{name: "duplicate name", sources: []migrations.Source{
			{Name: "core", TrackingTable: "a", FS: alphaFS, Dir: "alpha"},
			{Name: "core", TrackingTable: "b", FS: betaFS, Dir: "beta"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected NewCommand to panic")
				}
			}()
			NewCommand(tt.sources...)
		})
	}
}

func TestNewCommandAcceptsHyphenatedNames(t *testing.T) {
	// Downstream-registered sources may use hyphenated names.
	NewCommand(migrations.Source{Name: "extra-track", TrackingTable: "t", FS: alphaFS, Dir: "alpha"})
}

func TestResolveDSN(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		want    string
		wantErr bool
	}{
		{name: "flag wins over env", flag: "postgres://flag", env: "postgres://env", want: "postgres://flag"},
		{name: "env fallback", env: "postgres://env", want: "postgres://env"},
		{name: "neither set", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(dbURLEnv, tt.env)
			s := &commandState{dbURL: tt.flag}
			got, err := s.resolveDSN()
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveDSN() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("resolveDSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSource(t *testing.T) {
	multi := testSources()
	single := multi[:1]
	tests := []struct {
		name    string
		sources []migrations.Source
		flag    string
		want    string
		wantErr string
	}{
		{name: "single source inferred", sources: single, want: "alpha"},
		{name: "single source explicit match", sources: single, flag: "alpha", want: "alpha"},
		{name: "single source mismatch", sources: single, flag: "beta", wantErr: "not registered"},
		{name: "multi requires flag", sources: multi, wantErr: "Pass --source"},
		{name: "multi explicit", sources: multi, flag: "beta", want: "beta"},
		{name: "multi unknown", sources: multi, flag: "nope", wantErr: "not registered"},
		{name: "none registered", sources: nil, wantErr: "no migration sources"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcs := tt.sources
			s := &commandState{
				source:    tt.flag,
				resolveFn: func(context.Context) ([]migrations.Source, error) { return srcs, nil },
			}
			got, err := s.resolveSource(context.Background())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveSource() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSource() unexpected error: %v", err)
			}
			if got.Name != tt.want {
				t.Errorf("resolveSource() = %q, want %q", got.Name, tt.want)
			}
		})
	}
}

// TestArgValidation covers rejections that fire before any database access.
func TestArgValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "down non-integer", args: []string{"down", "abc"}, wantErr: "positive integer"},
		{name: "down zero", args: []string{"down", "0"}, wantErr: "positive integer"},
		{name: "goto non-integer", args: []string{"goto", "abc"}, wantErr: "non-negative integer"},
		{name: "goto negative", args: []string{"goto", "--", "-1"}, wantErr: "non-negative integer"},
		{name: "up rejects source flag", args: []string{"up", "--source", "alpha"}, wantErr: "--source is not applicable"},
		{name: "status rejects source flag", args: []string{"status", "--source", "alpha"}, wantErr: "--source is not applicable"},
		{name: "status invalid output", args: []string{"status", "--output", "yaml"}, wantErr: "invalid --output"},
		{name: "no dsn", args: []string{"version"}, wantErr: "set the database URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(dbURLEnv, "")
			_, _, err := runCLI(t, testSources(), tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestNewCommandFromFunc covers deferred source resolution: the resolver must
// not run at construction, and its failures surface as command errors rather
// than wiring-time panics.
func TestNewCommandFromFunc(t *testing.T) {
	t.Run("resolver is not called at construction", func(t *testing.T) {
		called := false
		NewCommandFromFunc(func(context.Context) ([]migrations.Source, error) {
			called = true
			return testSources(), nil
		})
		if called {
			t.Fatal("resolver ran during command construction")
		}
	})

	t.Run("resolver error surfaces as command error", func(t *testing.T) {
		cmd := NewCommandFromFunc(func(context.Context) ([]migrations.Source, error) {
			return nil, errors.New("cluster unreachable")
		})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"version", "--db-url", "postgres://unused"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "cluster unreachable") {
			t.Fatalf("error = %v, want resolver error", err)
		}
	})

	t.Run("duplicate names from resolver error instead of panicking", func(t *testing.T) {
		dup := []migrations.Source{
			{Name: "core", TrackingTable: "a", FS: alphaFS, Dir: "alpha"},
			{Name: "core", TrackingTable: "b", FS: betaFS, Dir: "beta"},
		}
		cmd := NewCommandFromFunc(func(context.Context) ([]migrations.Source, error) { return dup, nil })
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"version", "--db-url", "postgres://unused"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "appears twice") {
			t.Fatalf("error = %v, want duplicate-name error", err)
		}
	})
}

// TestStatusJSONShape freezes the `status -o json` wire format. Operators
// consume it via jq, so a rename or retype is a breaking change; update this
// test only with a deliberate contract change.
func TestStatusJSONShape(t *testing.T) {
	payload := statusJSON{
		Applied: 3,
		Pending: 1,
		Sources: []statusSourceJSON{{Name: "alpha", Applied: 2, Pending: 1, Version: 2, Downgraded: false}},
	}
	got, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"applied":3,"pending":1,"sources":[{"name":"alpha","applied":2,"pending":1,"version":2,"downgraded":false}]}`
	if string(got) != want {
		t.Errorf("status JSON shape changed:\n got: %s\nwant: %s", got, want)
	}
}

// --- database-backed tests ---

// TestCLIAgainstPostgres walks the operator surface end to end against a real
// Postgres: up, status, version, down, and goto. The
// subtests share one container and run in order — each builds on the schema
// state the previous one left.
func TestCLIAgainstPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}
	ctx := context.Background()
	dsn := dbtest.StartT(ctx, t)
	sources := testSources()

	mustContain := func(t *testing.T, out, want string) {
		t.Helper()
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
	mustRun := func(t *testing.T, args ...string) string {
		t.Helper()
		out, _, err := runCLI(t, sources, append(args, "--db-url", dsn)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}

	t.Run("up applies all sources", func(t *testing.T) {
		mustContain(t, mustRun(t, "up"), "schema is up to date")
	})

	t.Run("up is idempotent", func(t *testing.T) {
		mustContain(t, mustRun(t, "up"), "schema is up to date")
	})

	t.Run("status text", func(t *testing.T) {
		out := mustRun(t, "status")
		mustContain(t, out, "3 migration(s) applied, 0 pending")
		mustContain(t, out, "alpha: 2 applied (at v2), 0 pending")
		mustContain(t, out, "beta: 1 applied (at v1), 0 pending")
	})

	t.Run("status json", func(t *testing.T) {
		var got statusJSON
		if err := json.Unmarshal([]byte(mustRun(t, "status", "--output", "json")), &got); err != nil {
			t.Fatal(err)
		}
		if got.Applied != 3 || got.Pending != 0 || len(got.Sources) != 2 {
			t.Fatalf("unexpected status: %+v", got)
		}
	})

	t.Run("version per source and filtered", func(t *testing.T) {
		out := mustRun(t, "version")
		mustContain(t, out, "alpha: 2")
		mustContain(t, out, "beta: 1")
		mustContain(t, mustRun(t, "version", "--source", "alpha"), "2")
	})

	t.Run("down rolls back one step", func(t *testing.T) {
		mustContain(t, mustRun(t, "down", "1", "--source", "alpha"), "rolled back 1 migration(s)")
		mustContain(t, mustRun(t, "version", "--source", "alpha"), "1")
	})

	t.Run("goto moves forward", func(t *testing.T) {
		mustContain(t, mustRun(t, "goto", "2", "--source", "alpha"), "schema is at version 2")
	})

	t.Run("goto zero empties the source", func(t *testing.T) {
		mustContain(t, mustRun(t, "goto", "0", "--source", "beta"), "version 0 (no migrations applied)")
		mustContain(t, mustRun(t, "version", "--source", "beta"), "no migrations applied")
	})

	t.Run("down with nothing to roll back", func(t *testing.T) {
		mustContain(t, mustRun(t, "down", "1", "--source", "beta"), "no migrations to roll back")
	})

	t.Run("up restores a source after goto zero", func(t *testing.T) {
		mustContain(t, mustRun(t, "up"), "schema is up to date")
	})
}

func TestSourceFileVersions(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		want    []int64
		wantErr bool
	}{
		{"standard format", []string{"000001_create.sql", "000002_alter.sql"}, []int64{1, 2}, false},
		{"non-SQL ignored", []string{"README.md"}, nil, false},
		{"legacy split rejected", []string{"000001_create.up.sql"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := fstest.MapFS{}
			for _, f := range tt.files {
				mfs["m/"+f] = &fstest.MapFile{Data: migrationFile("SELECT 1;", "SELECT 1;")}
			}
			got, err := sourceFileVersions(migrations.Source{FS: mfs, Dir: "m"})
			if tt.wantErr {
				if err == nil {
					t.Fatal("sourceFileVersions succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] got %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}
