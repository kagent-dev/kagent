package postgressetup_test

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/kagent-dev/kagent/go/core/pkg/postgres"
	"github.com/kagent-dev/kagent/go/core/pkg/postgressetup"
)

const scriptPath = "/tmp/kagent-postgres-setup.sql"

func TestSetupScript(t *testing.T) {
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
	adminURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	if err := container.CopyToContainer(ctx, []byte(postgressetup.Script()), scriptPath, 0o600); err != nil {
		t.Fatalf("copy setup script: %v", err)
	}

	bundled := postgressetup.DefaultConfig()
	bundled.OwnerPassword = "test-owner-password"
	bundled.ReadWritePassword = "test-readwrite-password"
	for _, tc := range []struct {
		name   string
		config postgressetup.Config
	}{
		{name: "bundled identity names", config: bundled},
		{name: "operator values", config: postgressetup.Config{
			Schema:            "custom_kagent",
			OwnerRole:         "custom_kagent_owner",
			OwnerUser:         "custom kagent owner",
			OwnerPassword:     "custom owner's password",
			ReadWriteRole:     "custom_kagent_readwrite",
			ReadWriteUser:     "custom kagent readwrite",
			ReadWritePassword: "custom read/write password",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testSetupScript(t, container, adminURL, tc.config)
		})
	}
}

func testSetupScript(t *testing.T, container *tcpostgres.PostgresContainer, adminURL string, config postgressetup.Config) {
	ctx := t.Context()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	runSetupWith := func(config postgressetup.Config) error {
		command := []string{"psql", "--no-psqlrc", "--set=ON_ERROR_STOP=1", "--username", "postgres", "--dbname", "kagent_test"}
		command = append(command, config.PSQLArgs()...)
		command = append(command, "--file="+scriptPath)
		exitCode, output, err := container.Exec(ctx, command, tcexec.Multiplexed())
		if err != nil {
			return err
		}
		detail, err := io.ReadAll(output)
		if err != nil {
			return fmt.Errorf("read psql output: %w", err)
		}
		if exitCode != 0 {
			return fmt.Errorf("psql exited with code %d: %s", exitCode, strings.TrimSpace(string(detail)))
		}
		for _, password := range []string{config.OwnerPassword, config.ReadWritePassword} {
			if password != "" && strings.Contains(string(detail), password) {
				return fmt.Errorf("psql printed a password: %s", detail)
			}
		}
		return nil
	}
	runSetup := func() error { return runSetupWith(config) }
	exec := func(statement string) {
		t.Helper()
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	missingRole := config
	missingRole.OwnerRole = ""
	if err := runSetupWith(missingRole); err == nil || !strings.Contains(err.Error(), "run the script in one transaction") {
		t.Fatalf("setup without an owner role error = %v", err)
	}

	for name, shared := range map[string]func(*postgressetup.Config){
		"shared role":           func(c *postgressetup.Config) { c.ReadWriteRole = c.OwnerRole },
		"shared login":          func(c *postgressetup.Config) { c.ReadWriteUser = c.OwnerUser },
		"login named as a role": func(c *postgressetup.Config) { c.ReadWriteUser = c.OwnerRole },
	} {
		conflicting := config
		shared(&conflicting)
		if err := runSetupWith(conflicting); err == nil || !strings.Contains(err.Error(), "must all be different") {
			t.Fatalf("setup with a %s error = %v", name, err)
		}
	}

	ownerRole := pgx.Identifier{config.OwnerRole}.Sanitize()
	exec("CREATE ROLE " + ownerRole + " LOGIN")
	if err := runSetup(); err == nil || !strings.Contains(err.Error(), "conflicts with the required attributes") {
		t.Fatalf("setup with an incompatible role error = %v", err)
	}
	exec("DROP ROLE " + ownerRole)

	schema := pgx.Identifier{config.Schema}.Sanitize()
	exec("CREATE SCHEMA " + schema)
	if err := runSetup(); err == nil || !strings.Contains(err.Error(), "is owned by") {
		t.Fatalf("setup with an incompatible schema error = %v", err)
	}
	exec("DROP SCHEMA " + schema)

	for i := range 2 {
		if err := runSetup(); err != nil {
			t.Fatalf("setup run %d: %v", i+1, err)
		}
	}

	loginURL := func(user, password string) string {
		t.Helper()
		u, err := url.Parse(adminURL)
		if err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword(user, password)
		return u.String()
	}
	ownerURL := loginURL(config.OwnerUser, config.OwnerPassword)
	readWriteURL := loginURL(config.ReadWriteUser, config.ReadWritePassword)

	// The logins have no privileges of their own until they assume their roles.
	login, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatalf("connect as %s: %v", config.OwnerUser, err)
	}
	defer login.Close(ctx)
	if _, err := login.Exec(ctx, "CREATE TABLE "+schema+".forbidden (id integer)"); err == nil {
		t.Fatal("the owner login created a table without assuming the owner role")
	}

	sources := migrations.BuiltinSourcesInSchema(false, config.Schema, "")
	ownerConfig, err := postgres.PoolConfig(ownerURL, config.OwnerRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.RunUp(ctx, ownerConfig, sources); err != nil {
		t.Fatalf("migrate as %s: %v", config.OwnerRole, err)
	}

	// The read/write role verifies migrations and writes rows, but cannot change the schema.
	readWriteConfig, err := postgres.PoolConfig(readWriteURL, config.ReadWriteRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.VerifyMigrated(ctx, readWriteConfig, sources); err != nil {
		t.Fatalf("verify as %s: %v", config.ReadWriteRole, err)
	}
	readWrite, err := pgxpool.NewWithConfig(ctx, readWriteConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer readWrite.Close()
	table := schema + ".a2a_context"
	for _, statement := range []string{
		"INSERT INTO " + table + " (id, context_id) VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002')",
		"UPDATE " + table + " SET context_id = '00000000-0000-0000-0000-000000000003'",
		"SELECT context_id FROM " + table,
		"DELETE FROM " + table,
	} {
		if _, err := readWrite.Exec(ctx, statement); err != nil {
			t.Fatalf("%s as %s: %v", statement, config.ReadWriteRole, err)
		}
	}
	if _, err := readWrite.Exec(ctx, "CREATE TABLE "+schema+".forbidden (id integer)"); err == nil {
		t.Fatalf("%s created a table", config.ReadWriteRole)
	}
	var owner string
	if err := admin.QueryRow(ctx, "SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid = $1::regclass", schema+".session").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != config.OwnerRole {
		t.Fatalf("session table owner = %q, want %q", owner, config.OwnerRole)
	}
}
