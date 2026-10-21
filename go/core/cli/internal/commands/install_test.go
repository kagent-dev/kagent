package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TestUninstallReversesInstall(t *testing.T) {
	const (
		listReleases   = "helm list --all-namespaces --filter ^kagent$ --output json"
		getDatabase    = "kubectl -n kagent get deployment/kagent-postgresql --ignore-not-found -o name"
		deleteDatabase = "kubectl delete --ignore-not-found -f -"
		bundled        = "deployment.apps/kagent-postgresql"
	)
	before := []string{
		listReleases,
		"kubectl delete sandboxtemplates.api.kagent.dev --all --all-namespaces --ignore-not-found --timeout=5m",
		"helm uninstall kagent --namespace kagent --wait",
		"kubectl delete sandboxconfigs.ate.dev --all --ignore-not-found --timeout=5m",
		"helm uninstall substrate --namespace ate-system --wait",
		getDatabase,
	}
	after := []string{
		"helm uninstall substrate-podcert --namespace podcertificate-controller-system --wait",
		"helm uninstall substrate-crds --namespace ate-system --wait",
		"helm uninstall kagent-crds --namespace kagent --wait",
		`kubectl patch namespace kagent --type=merge -p {"metadata":{"labels":{"ate.dev/api-access":null}}}`,
		"kubectl delete namespace podcertificate-controller-system ate-system --ignore-not-found",
		"kubectl delete clustertrustbundles.certificates.k8s.io " + strings.Join(substrateTrustBundles, " ") + " --ignore-not-found --timeout=5m",
	}
	for _, tc := range []struct {
		name string
		// releases is what helm list prints for kagent releases in every namespace.
		releases string
		// deployment is what kubectl get prints for the bundled PostgreSQL Deployment.
		deployment string
		// crdsGone makes kubectl answer as if no CRD served the resource.
		crdsGone bool
		// namespaceGone makes kubectl answer as if the kagent namespace did not exist.
		namespaceGone bool
		want          []string
		wantErr       string
	}{
		{
			name:       "deletes the bundled PostgreSQL",
			releases:   `[{"name":"kagent","namespace":"kagent"}]`,
			deployment: bundled,
			want:       slices.Concat(before, []string{deleteDatabase}, after),
		},
		{
			// After --skip-database-setup, Secrets with the bundled names belong to the operator.
			name:     "after --skip-database-setup, deletes no database",
			releases: `[{"name":"kagent","namespace":"kagent"}]`,
			want:     slices.Concat(before, after),
		},
		{
			// A rerun after the CRDs and the kagent namespace are gone finishes the rest.
			name:          "rerun with the CRDs and kagent namespace already deleted",
			releases:      `[]`,
			crdsGone:      true,
			namespaceGone: true,
			want:          slices.Concat(before, after),
		},
		{
			name:     "kagent installed in another namespace",
			releases: `[{"name":"kagent","namespace":"other"}]`,
			want:     []string{listReleases},
			wantErr:  "kagent is installed in namespace other, not kagent; rerun with --namespace other",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "commands.log")
			for _, tool := range []string{"helm", "kubectl"} {
				script := fmt.Sprintf(`#!/bin/sh
printf '%%s %%s\n' %s "$*" >> "$KAGENT_TEST_LOG"
case "$*" in
  "list "*) printf '%%s\n' "$KAGENT_TEST_RELEASES" ;;
  *"get deployment/kagent-postgresql"*) printf '%%s\n' "$KAGENT_TEST_DEPLOYMENT" ;;
  *"delete sandbox"*) [ -n "$KAGENT_TEST_CRDS_GONE" ] && { echo 'error: the server doesn'"'"'t have a resource type "sandboxtemplates"'; exit 1; } ;;
  *"patch namespace kagent"*) [ -n "$KAGENT_TEST_NAMESPACE_GONE" ] && { echo 'Error from server (NotFound): namespaces "kagent" not found'; exit 1; } ;;
esac
cat >/dev/null
`, tool)
				require.NoError(t, os.WriteFile(filepath.Join(dir, tool), []byte(script), 0o700))
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("KAGENT_TEST_LOG", logPath)
			t.Setenv("KAGENT_TEST_RELEASES", tc.releases)
			t.Setenv("KAGENT_TEST_DEPLOYMENT", tc.deployment)
			if tc.crdsGone {
				t.Setenv("KAGENT_TEST_CRDS_GONE", "1")
			}
			if tc.namespaceGone {
				t.Setenv("KAGENT_TEST_NAMESPACE_GONE", "1")
			}

			var err error
			stdout := captureStdout(t, func() {
				err = runUninstall(t.Context(), "kagent", "postgres:18-alpine")
			})
			commands, readErr := os.ReadFile(logPath)
			require.NoError(t, readErr)
			require.Equal(t, tc.want, strings.Split(strings.TrimSpace(string(commands)), "\n"))
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Contains(t, stdout, "kubectl delete namespace kagent")
		})
	}
}

// captureStdout returns what fn writes to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	fn()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
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

	require.NoError(t, prepareBundledPostgres(t.Context(), "demo", "mirror.example/library/postgres:18-alpine"))
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
	require.ErrorContains(t, prepareBundledPostgres(t.Context(), "bad\nnamespace: injected", "postgres:18-alpine"), "invalid namespace")
}

func TestBundledPostgresImage(t *testing.T) {
	manifest, err := bundledPostgresManifest("demo", "mirror.example/library/postgres:18-alpine")
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(manifest, "image: mirror.example/library/postgres:18-alpine"))

	_, err = bundledPostgresManifest("demo", "postgres:18\n        command: [sh]")
	require.ErrorContains(t, err, "invalid PostgreSQL image")
}

func TestBundledPostgresAssets(t *testing.T) {
	cfg := &configv1.CLIConfig{}
	config.DefaultCLI(cfg)
	manifest, err := bundledPostgresManifest("demo", cfg.Install.GetBundledPostgresImage())
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
		require.Contains(t, bundledPostgresSetupArgs(), want)
	}
	require.Contains(t, bundledPostgresSetupSQL(), ":'kagent_schema'")
	require.Contains(t, bundledPostgresSetupSQL(), ":'substrate_schema'")
}

func TestInstallDatabaseAndHelmOverrides(t *testing.T) {
	cmd := NewInstallCmd()
	require.NotNil(t, cmd.Flags().Lookup("skip-database-setup"))

	chart := &configv1.ChartConfig{Repository: new("./charts/"), Version: new("v1.2.3"), ValuesFiles: []string{"custom.yaml"}}
	config := setupHelmConfig(chart, "OpenAI", "fake")
	require.Contains(t, config.inlineValues, "substrateClientCertificate:\n    enabled: true")
	require.Equal(t, []string{"custom.yaml"}, config.valuesFiles)
	require.Equal(t, "./charts/", config.registry)
	require.Equal(t, "v1.2.3", config.version)
	require.Equal(t, []string{"global.imageRegistry=mirror.example", "postgresClients[0]=demo/kagent-controller=kagent_owner_user", "postgresClients[1]=demo/kagent-controller=kagent_readwrite_user"}, podCertificateChartValues([]string{"rustfs.enabled=false", "global.imageRegistry=mirror.example"}, "demo", true))
}

func TestInstallUsesSeparateSubstrateReleases(t *testing.T) {
	originalPrepareSubstrate := prepareSubstrate
	prepareSubstrate = func(context.Context, string) error { return nil }
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

	cfg := connection.DefaultOptions()
	// The fake kubectl fails port-forward, so only the API check after the releases fails.
	err := install(t.Context(), &cfg,
		helmConfig{registry: "./kagent/", version: "v1"},
		helmConfig{registry: "./substrate/", version: "v2"},
		helmConfig{registry: "./podcert/", version: "v3"},
		"openAI", false, "postgres:18-alpine",
	)
	require.ErrorContains(t, err, "start port-forward")

	commands, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, []string{
		"upgrade --install kagent-crds ./kagent/kagent-crds --version v1 --namespace kagent --create-namespace --wait --wait-for-jobs --history-max 2 --timeout 5m",
		"upgrade --install substrate-crds ./substrate/substrate-crds --version v2 --namespace ate-system --create-namespace --wait --wait-for-jobs --history-max 2 --timeout 5m",
		"upgrade --install substrate-podcert ./podcert/substrate-podcert --version v3 --namespace podcertificate-controller-system --create-namespace --wait --wait-for-jobs --history-max 2 --timeout 5m --set postgresClients[0]=kagent/kagent-controller=kagent_owner_user --set postgresClients[1]=kagent/kagent-controller=kagent_readwrite_user",
		"upgrade --install substrate ./substrate/substrate --version v2 --namespace ate-system --create-namespace --wait --wait-for-jobs --history-max 2 --timeout 5m --set postgres.clientCertificates.enabled=true -f -",
		"upgrade --install kagent ./kagent/kagent --version v1 --namespace kagent --create-namespace --wait --wait-for-jobs --history-max 2 --timeout 5m --set database.postgres.clientCertificate.enabled=true -f -",
	}, strings.Split(strings.TrimSpace(string(commands)), "\n"))
}

func TestInstallValuesFilesFollowDefaults(t *testing.T) {
	dir := t.TempDir()
	argsPath, valuesPath := filepath.Join(dir, "args"), filepath.Join(dir, "values")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helm"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$KAGENT_TEST_HELM_ARGS\"\ncat > \"$KAGENT_TEST_HELM_VALUES\"\n"), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KAGENT_TEST_HELM_ARGS", argsPath)
	t.Setenv("KAGENT_TEST_HELM_VALUES", valuesPath)
	defaults := "config:\n  logging: {level: info}\n"
	_, err := installChart(t.Context(), "kagent", "team", "./charts/", "v1", nil, []string{"operator.yaml", "local.yaml"}, defaults)
	require.NoError(t, err)
	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(string(args), "-f\n-\n-f\noperator.yaml\n-f\nlocal.yaml\n"), string(args))
	values, err := os.ReadFile(valuesPath)
	require.NoError(t, err)
	require.Equal(t, defaults, string(values))
}

func TestSubstrateInstallValues(t *testing.T) {
	for _, namespace := range []string{"kagent", "team-a"} {
		t.Run(namespace, func(t *testing.T) {
			var values struct {
				AteAPI struct {
					Authorization struct {
						BootstrapOwners []string `json:"bootstrapOwners"`
					} `json:"authorization"`
				} `json:"ateApi"`
				CredentialProvider struct {
					NamespacePolicies []struct {
						Atespace          string   `json:"atespace"`
						AllowedNamespaces []string `json:"allowedNamespaces"`
					} `json:"namespacePolicies"`
				} `json:"credentialProvider"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(substrateInstallValues(namespace)), &values))
			require.Equal(t, []string{"spiffe://cluster.local/ns/" + namespace + "/sa/kagent-controller"}, values.AteAPI.Authorization.BootstrapOwners)
			require.Len(t, values.CredentialProvider.NamespacePolicies, 1)
			require.Equal(t, namespace, values.CredentialProvider.NamespacePolicies[0].Atespace)
			require.Equal(t, []string{namespace}, values.CredentialProvider.NamespacePolicies[0].AllowedNamespaces)
		})
	}
}
