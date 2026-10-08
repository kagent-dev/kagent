package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
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
  *"exec -i deployment/kagent-postgresql"*)
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
		"-n demo rollout status deployment/kagent-postgresql --timeout=5m",
		"-n demo exec -i deployment/kagent-postgresql -c postgresql -- psql",
		"--set=substrate_schema=substrate",
		"--set=substrate_owner_role=substrate_owner",
		"--set=substrate_owner_user=substrate_owner_user",
		"--set=substrate_owner_password=",
		"--set=substrate_readwrite_role=substrate_readwrite",
		"--set=substrate_readwrite_user=substrate_readwrite_user",
		"--set=substrate_readwrite_password=",
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
		strings.NewReplacer("${NAMESPACE}", "demo", "${SUBSTRATE_NAMESPACE}", substrateNamespace).Replace(bundledPostgresYAML),
	}
	manifest := strings.Join(manifests, "\n---\n")
	for _, document := range strings.Split(manifest, "\n---\n") {
		require.NotContains(t, document, "${")
		var object map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(document), &object))
		require.NotEmpty(t, object["kind"])
	}
	require.Contains(t, manifest, "hostssl all postgres all reject")
	require.Contains(t, manifest, "postgres.podcert.ate.dev/identity")
	require.Contains(t, manifest, "sslmode=verify-full")
	require.Contains(t, manifest, "name: kagent-postgres")
	require.Contains(t, manifest, "name: substrate-postgres-readwrite")
	require.Contains(t, manifest, "name: substrate-postgres-owner")
	require.Contains(t, manifest, "namespace: ate-system")
	for _, want := range []string{"kagent_owner", "kagent_user", "substrate_owner", "substrate_readwrite"} {
		require.Contains(t, bundledPostgresSetupSQL(), want)
	}
	require.NotContains(t, bundledPostgresSQL, "substrate_owner")
}

func TestInstallDatabaseAndHelmOverrides(t *testing.T) {
	cmd := NewInstallCmd()
	require.NotNil(t, cmd.Flags().Lookup("skip-database-setup"))

	t.Setenv("KAGENT_HELM_EXTRA_ARGS", "--set ui.replicas=0")
	config := setupHelmConfig("openAI", "fake")
	require.Contains(t, config.values, "controller.substrate.enabled=true")
	require.Contains(t, config.values, "ui.replicas=0")
	require.Empty(t, crdChartValues(config.values))
	require.Equal(t, []string{"global.imageRegistry=mirror.example", "postgresClients[0]=demo/kagent-controller=kagent_user"}, podCertificateChartValues([]string{"rustfs.enabled=false", "global.imageRegistry=mirror.example"}, "demo", true))

	t.Setenv("KAGENT_SUBSTRATE_HELM_REPO", "./substrate-charts/")
	t.Setenv("KAGENT_SUBSTRATE_HELM_VERSION", "v1.2.3")
	t.Setenv("KAGENT_SUBSTRATE_HELM_EXTRA_ARGS", "--set rustfs.enabled=false")
	substrateConfig := setupSubstrateHelmConfig()
	require.Equal(t, "./substrate-charts/", substrateConfig.registry)
	require.Equal(t, "v1.2.3", substrateConfig.version)
	require.Equal(t, []string{"rustfs.enabled=false"}, substrateConfig.values)

	t.Setenv("KAGENT_SUBSTRATE_PODCERT_HELM_REPO", "./podcert-charts/")
	t.Setenv("KAGENT_SUBSTRATE_PODCERT_HELM_VERSION", "v4.5.6")
	podCertificateConfig := setupPodCertificateHelmConfig(substrateConfig)
	require.Equal(t, "./podcert-charts/", podCertificateConfig.registry)
	require.Equal(t, "v4.5.6", podCertificateConfig.version)
}

func TestInstallUsesSeparateSubstrateReleases(t *testing.T) {
	originalPrepareSubstrate := prepareSubstrate
	prepareSubstrate = func(context.Context) error { return nil }
	t.Cleanup(func() { prepareSubstrate = originalPrepareSubstrate })

	dir := t.TempDir()
	logPath := filepath.Join(dir, "helm.log")
	helm := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$KAGENT_TEST_HELM_LOG\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helm"), []byte(helm), 0o700))
	kubectl := `#!/bin/sh
case "$*" in
  *"port-forward"*) exit 1 ;;
esac
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(kubectl), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KAGENT_TEST_HELM_LOG", logPath)
	t.Setenv("KAGENT_SUBSTRATE_PODCERT_HELM_REPO", "./podcert/")
	t.Setenv("KAGENT_SUBSTRATE_PODCERT_HELM_VERSION", "v3")

	cfg := connection.DefaultOptions()
	install(t.Context(), &cfg,
		helmConfig{registry: "./kagent/", version: "v1"},
		helmConfig{registry: "./substrate/", version: "v2"},
		"openAI", false,
	)

	commands, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, []string{
		"upgrade --install kagent-crds ./kagent/kagent-crds --version v1 --namespace kagent --create-namespace --wait --history-max 2 --timeout 5m",
		"upgrade --install substrate-crds ./substrate/substrate-crds --version v2 --namespace ate-system --create-namespace --wait --history-max 2 --timeout 5m",
		"upgrade --install substrate-podcert ./podcert/substrate-podcert --version v3 --namespace podcertificate-controller-system --create-namespace --wait --history-max 2 --timeout 5m --set postgresClients[0]=kagent/kagent-controller=kagent_user",
		"upgrade --install substrate ./substrate/substrate --version v2 --namespace ate-system --create-namespace --wait --history-max 2 --timeout 5m --set credentialProvider.namespacePolicies[0].atespace=kagent --set credentialProvider.namespacePolicies[0].allowedNamespaces[0]=kagent --set postgres.clientCertificates.enabled=true",
		"upgrade --install kagent ./kagent/kagent --version v1 --namespace kagent --create-namespace --wait --history-max 2 --timeout 5m --set substrateWorkerPool.create=true --set substrateWorkerPool.workerImage=ghcr.io/kagent-dev/substrate/ateom-gvisor:v2 --set database.postgres.clientCertificate.enabled=true",
	}, strings.Split(strings.TrimSpace(string(commands)), "\n"))
}
