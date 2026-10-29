package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/cli"
)

func installConfigWithLocalCharts(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"kagent", "kagent-crds", "substrate", "substrate-crds", "substrate-podcert"} {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: "+name+"\nversion: 0.0.0\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "kagent", "templates", "controller-deployment.yaml"), []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: controller}\nspec: {template: {spec: {serviceAccountName: configured-controller}}}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "operator.yaml"), []byte("controller: {replicas: 2}\nuserProvidedValue: retained\n"), 0o600))
	config := fmt.Sprintf(`install:
  defaultModelProvider: OpenAI
  kagent:
    repository: %q
    version: 0.0.0
    valuesFiles: [operator.yaml]
  substrate:
    repository: %q
    version: 0.0.0
  podCertificate:
    repository: %q
    version: 0.0.0
`, root, root, root)
	path := filepath.Join(root, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(config), 0o600))
	return path
}

func TestInstallDryRunWithoutClusterOrAPIKey(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for dry-run integration")
	}
	for _, tc := range []struct {
		apiKey       string
		skipDatabase bool
	}{
		{"", false},
		{"secret-that-must-not-be-exported", false},
		{"", true},
		{"secret-that-must-not-be-exported", true},
	} {
		t.Run(fmt.Sprintf("key_set=%t/skip_database=%t", tc.apiKey != "", tc.skipDatabase), func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", tc.apiKey)
			t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing-kubeconfig"))
			config := installConfigWithLocalCharts(t)
			destination := filepath.Join(t.TempDir(), "export")
			root := cli.Root()
			var out, stderr bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&stderr)
			args := []string{"--config", config, "install", "--dry-run", "--profile", "minimal", "--output-dir", destination}
			if tc.skipDatabase {
				args = append(args, "--skip-database-setup")
			}
			root.SetArgs(args)
			require.NoError(t, root.ExecuteContext(t.Context()))
			require.Equal(t, "Wrote Helm commands and values to "+destination+"\n", out.String())
			require.Empty(t, stderr.String())
			values, err := os.ReadFile(filepath.Join(destination, "values", "kagent.yaml"))
			require.NoError(t, err)
			require.Contains(t, string(values), "${OPENAI_API_KEY}")
			require.Contains(t, string(values), "userProvidedValue: retained")
			require.Contains(t, string(values), "replicas: 2")
			require.Contains(t, string(values), "k8s-agent:\n  enabled: false")
			require.NotContains(t, string(values), "secret-that-must-not-be-exported")
			podcert, err := os.ReadFile(filepath.Join(destination, "values", "substrate-podcert.yaml"))
			require.NoError(t, err)
			readme, err := os.ReadFile(filepath.Join(destination, "README.md"))
			require.NoError(t, err)
			require.Contains(t, string(readme), "Complete the prerequisite steps in the Helm installation guide: https://kagent.dev/docs/kagent/1.x/setup/installation/#before-you-begin.")
			require.Contains(t, string(readme), "`OPENAI_API_KEY`: `providers.openAI.apiKey` in the `kagent` release")
			require.NotContains(t, string(readme), "secret-that-must-not-be-exported")
			if tc.skipDatabase {
				require.NotContains(t, string(podcert), "postgresClients")
				require.Contains(t, string(readme), "This export was generated with `--skip-database-setup`")
			} else {
				require.Contains(t, string(podcert), "postgresClients")
				require.Contains(t, string(readme), "The values configure certificate authentication for the bundled database.")
			}
		})
	}
}

func TestOutputDirectoryRequiresDryRun(t *testing.T) {
	config := installConfigWithLocalCharts(t)
	dir := filepath.Join(t.TempDir(), "export")
	_, err := executeConfig(t, "--config", config, "install", "--output-dir", dir)
	require.ErrorContains(t, err, "--output-dir requires --dry-run")
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err))
}

func TestDryRunFlagsAreIndependent(t *testing.T) {
	first, second := cli.Root(), cli.Root()
	findInstall := func(root *cobra.Command) *cobra.Command {
		cmd, _, err := root.Find([]string{"install"})
		require.NoError(t, err)
		return cmd
	}
	firstInstall, secondInstall := findInstall(first), findInstall(second)
	require.NoError(t, firstInstall.Flags().Set("output-dir", "custom-export"))
	require.NoError(t, firstInstall.Flags().Set("dry-run", "true"))
	require.Equal(t, "kagent-install", secondInstall.Flags().Lookup("output-dir").Value.String())
	require.Equal(t, "false", secondInstall.Flags().Lookup("dry-run").Value.String())
}
