package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestDeleteCRDsLeavesLegacyGroupAlone(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "kubectl.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$KAGENT_TEST_KUBECTL_LOG\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KAGENT_TEST_KUBECTL_LOG", logPath)

	require.NoError(t, deleteCRDs(t.Context()))
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"delete crd agents.api.kagent.dev",
		"delete crd agenttemplates.api.kagent.dev",
		"delete crd harnesses.api.kagent.dev",
		"delete crd modelconfigs.api.kagent.dev",
		"delete crd modelproviderconfigs.api.kagent.dev",
		"delete crd remotemcpservers.api.kagent.dev",
	}, strings.Split(strings.TrimSpace(string(data)), "\n"))
}

func TestPrepareBundledPostgres(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "kubectl.log")
	sqlPath := filepath.Join(dir, "setup.sql")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$KAGENT_TEST_KUBECTL_LOG"
case "$*" in
  *"get secret postgres-admin"*)
    echo 'Error from server (NotFound): secrets "postgres-admin" not found' >&2
    exit 1
    ;;
  *"exec deployment/kagent-postgresql"*)
    cat > "$KAGENT_TEST_SQL"
    ;;
  *)
    cat >/dev/null
    ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KAGENT_TEST_KUBECTL_LOG", logPath)
	t.Setenv("KAGENT_TEST_SQL", sqlPath)

	require.NoError(t, prepareBundledPostgres(t.Context(), "demo"))
	commands, err := os.ReadFile(logPath)
	require.NoError(t, err)
	for _, want := range []string{
		"apply --server-side --field-manager=kagent-cli -f -",
		"-n demo get secret postgres-admin",
		"-n demo rollout status deployment/kagent-postgresql --timeout=5m",
		"-n demo exec deployment/kagent-postgresql -c postgresql -- psql",
	} {
		require.Contains(t, string(commands), want)
	}
	sql, err := os.ReadFile(sqlPath)
	require.NoError(t, err)
	require.Contains(t, string(sql), "CREATE ROLE %I")
	require.Contains(t, string(sql), "substrate_readwrite_user")
}

func TestPrepareBundledPostgresRejectsInvalidNamespace(t *testing.T) {
	require.ErrorContains(t, prepareBundledPostgres(t.Context(), "bad\nnamespace: injected"), "invalid namespace")
}

func TestBundledPostgresAssets(t *testing.T) {
	manifests := []string{
		strings.ReplaceAll(bundledPostgresNamespaceYAML, "${NAMESPACE}", "demo"),
		strings.NewReplacer("${NAMESPACE}", "demo", "${PASSWORD}", "secret").Replace(bundledPostgresAdminSecretYAML),
		strings.ReplaceAll(bundledPostgresYAML, "${NAMESPACE}", "demo"),
	}
	manifest := strings.Join(manifests, "\n---\n")
	for _, document := range strings.Split(manifest, "\n---\n") {
		require.NotContains(t, document, "${")
		var object map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(document), &object))
		require.NotEmpty(t, object["kind"])
	}
	require.Contains(t, manifest, "host all postgres all reject")
	require.Contains(t, manifest, "name: kagent-postgres")
	require.Contains(t, manifest, "name: substrate-postgres-readwrite")
	require.Contains(t, manifest, "name: substrate-postgres-owner")
	for _, want := range []string{"kagent_owner", "kagent_user", "substrate_owner", "substrate_readwrite"} {
		require.Contains(t, bundledPostgresSetupSQL(), want)
	}
	require.NotContains(t, bundledPostgresSQL, "substrate_owner")
}

func TestInstallDatabaseAndHelmOverrides(t *testing.T) {
	cmd := NewInstallCmd()
	require.NotNil(t, cmd.Flags().Lookup("skip-database-setup"))

	t.Setenv("KAGENT_HELM_EXTRA_ARGS", "--set substrate.enabled=true --set ui.replicas=0")
	config := setupHelmConfig("openAI", "fake")
	require.Contains(t, config.values, "substrate.enabled=true")
	require.Contains(t, config.values, "ui.replicas=0")
	require.Equal(t, []string{"substrate.enabled=true"}, crdChartValues(config.values))
}
