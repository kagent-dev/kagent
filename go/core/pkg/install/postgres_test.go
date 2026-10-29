package install

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TestBundledPostgresImage(t *testing.T) {
	manifest, err := (Postgres{Namespace: "demo", Image: "mirror.example/library/postgres:18-alpine"}).Manifest()
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(manifest, "image: mirror.example/library/postgres:18-alpine"))

	_, err = (Postgres{Namespace: "demo", Image: "postgres:18\n        command: [sh]"}).Manifest()
	require.ErrorContains(t, err, "invalid PostgreSQL image")
}

func TestPostgresExtensions(t *testing.T) {
	p := Postgres{Namespace: "demo", Image: "postgres:18-alpine",
		ExtraHBA:   []string{"hostssl audit audit_user all scram-sha-256"},
		ExtraSetup: []SQLScript{{SQL: "SELECT 'application setup';", Args: []string{"--set=app_schema=app"}}},
	}
	manifest, err := p.Manifest()
	require.NoError(t, err)
	require.Contains(t, manifest, "all cert\n    hostssl audit audit_user all scram-sha-256\n")
	script := p.SetupScript()
	require.True(t, strings.HasPrefix(script.SQL, "BEGIN;\n"))
	require.True(t, strings.HasSuffix(script.SQL, "SELECT 'application setup';\nCOMMIT;\n"))
	require.Less(t, strings.Index(script.SQL, "kagent_schema"), strings.Index(script.SQL, "substrate_schema"))
	require.Less(t, strings.Index(script.SQL, "substrate_schema"), strings.Index(script.SQL, "application setup"))
	require.Contains(t, script.Args, "--set=app_schema=app")

	runner := &recordingRunner{}
	client := NewClient(runner, fake.NewSimpleClientset())
	require.NoError(t, client.RunSQL(t.Context(), "demo", "audit", SQLScript{SQL: "SELECT 1;", Args: []string{"--single-transaction"}}))
	require.Equal(t, Command{Program: "kubectl", Args: []string{"-n", "demo", "exec", "-i", "deployment/kagent-postgresql", "-c", "postgresql", "--", "psql", "--no-psqlrc", "--set=ON_ERROR_STOP=1", "--username", "postgres", "--dbname", "audit", "--single-transaction"}, Stdin: "SELECT 1;"}, runner.commands[0])
}

func TestPostgresRejectsInvalidInputBeforeExecution(t *testing.T) {
	runner := &recordingRunner{}
	client := NewClient(runner, fake.NewSimpleClientset())
	require.ErrorContains(t, client.PreparePostgres(t.Context(), Postgres{Namespace: "bad\nnamespace", Image: "postgres:18"}), "invalid namespace")
	require.Empty(t, runner.commands)
}

func TestRunSQLFailureDoesNotIncludeArguments(t *testing.T) {
	failure := errors.New("psql failed")
	runner := &recordingRunner{run: func(Command) (string, error) {
		return "command output", failure
	}}
	client := NewClient(runner, fake.NewSimpleClientset())
	password := "private-test-password"
	script := SQLScript{SQL: "SELECT 1;", Args: []string{"--set=setup_password=" + password}}
	err := client.RunSQL(t.Context(), "demo", "audit", script)
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "failed to prepare PostgreSQL database audit")
	require.NotContains(t, err.Error(), password)
	require.NotContains(t, err.Error(), "command output")
	require.Len(t, runner.commands, 1)
	require.Contains(t, runner.commands[0].Args, "--set=setup_password="+password)
	require.Equal(t, script.SQL, runner.commands[0].Stdin)
}

func TestPostgresDeleteStopsWhenManifestInvalid(t *testing.T) {
	runner := &recordingRunner{}
	client := NewClient(runner, fake.NewSimpleClientset())
	require.ErrorContains(t, client.deleteBundledPostgres(t.Context(), "demo", "bad:image:reference"), "invalid PostgreSQL image")
	require.Empty(t, runner.commands)
	failure := errors.New("delete failed")
	runner.run = func(Command) (string, error) { return "", failure }
	require.ErrorIs(t, client.deleteBundledPostgres(t.Context(), "demo", "postgres:18"), failure)
}

func TestBundledPostgresAssets(t *testing.T) {
	cfg := &configv1.CLIConfig{}
	config.DefaultCLI(cfg)
	manifest, err := (Postgres{Namespace: "demo", Image: cfg.Install.GetBundledPostgresImage()}).Manifest()
	require.NoError(t, err)
	for document := range strings.SplitSeq(manifest, "\n---\n") {
		require.NotContains(t, document, "${")
		var object map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(document), &object))
		require.NotEmpty(t, object["kind"])
	}
	require.Contains(t, manifest, "hostssl all postgres all reject")
	require.Contains(t, manifest, "postgres.podcert.ate.dev/identity")
	require.Contains(t, manifest, "sslmode=verify-full")
	require.Contains(t, manifest, "name: kagent-postgres-owner")
	require.Contains(t, manifest, "name: kagent-postgres-readwrite")
	require.Contains(t, manifest, "name: substrate-postgres-readwrite")
	require.Contains(t, manifest, "name: substrate-postgres-owner")
	require.Contains(t, manifest, "namespace: ate-system")
	for _, want := range []string{"--set=kagent_owner_role=kagent_owner", "--set=kagent_owner_user=kagent_owner_user", "--set=kagent_readwrite_role=kagent_readwrite", "--set=kagent_readwrite_user=kagent_readwrite_user", "--set=substrate_owner_role=substrate_owner", "--set=substrate_readwrite_role=substrate_readwrite"} {
		require.Contains(t, (Postgres{}).SetupScript().Args, want)
	}
	require.Contains(t, (Postgres{}).SetupScript().SQL, ":'kagent_schema'")
	require.Contains(t, (Postgres{}).SetupScript().SQL, ":'substrate_schema'")
}

func TestExtraHBARequiresApplicationRule(t *testing.T) {
	original := bundledPostgresYAML
	t.Cleanup(func() { bundledPostgresYAML = original })
	bundledPostgresYAML = strings.ReplaceAll(bundledPostgresYAML, "hostssl kagent kagent_owner_user,kagent_readwrite_user,substrate_owner_user,substrate_readwrite_user all cert", "hostssl kagent replacement_user all cert")
	p := Postgres{Namespace: "demo", Image: "postgres:18", ExtraHBA: []string{"hostssl audit audit_user all scram-sha-256"}}
	_, err := p.Manifest()
	require.ErrorContains(t, err, "no longer contains the application pg_hba.conf line")
	p.ExtraHBA = nil
	_, err = p.Manifest()
	require.NoError(t, err)
}
