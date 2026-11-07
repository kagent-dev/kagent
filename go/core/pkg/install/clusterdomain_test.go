package install

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func TestCorefileClusterDomain(t *testing.T) {
	scenarios := []struct {
		name     string
		corefile string
		want     string
	}{
		{
			name:     "Given a default Corefile, when parsing it, assert that the kubernetes plugin zone is returned",
			corefile: ".:53 {\n    errors\n    kubernetes cluster.local in-addr.arpa ip6.arpa {\n       pods insecure\n    }\n}\n",
			want:     "cluster.local",
		},
		{
			name:     "Given a custom zone with a trailing dot, when parsing it, assert that the zone is returned without the dot",
			corefile: ".:53 {\n    kubernetes corp.example. in-addr.arpa ip6.arpa\n}\n",
			want:     "corp.example",
		},
		{
			name:     "Given only reverse zones, when parsing it, assert that no domain is returned",
			corefile: ".:53 {\n    kubernetes in-addr.arpa ip6.arpa {\n    }\n}\n",
			want:     "",
		},
		{
			name:     "Given a commented-out kubernetes line, when parsing it, assert that the commented zone is ignored",
			corefile: ".:53 {\n    # kubernetes old.example in-addr.arpa\n    kubernetes new.example\n}\n",
			want:     "new.example",
		},
		{
			name:     "Given a Corefile without the kubernetes plugin, when parsing it, assert that no domain is returned",
			corefile: ".:53 {\n    forward . /etc/resolv.conf\n}\n",
			want:     "",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.want, corefileClusterDomain(scenario.corefile))
		})
	}
}

func TestPlanClusterDomain(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for values integration")
	}
	// Helm splits -f arguments on commas, which sentence-named subtest
	// directories contain, so caller files live in the parent test directory.
	files := t.TempDir()
	coreDNS := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns"},
		Data:       map[string]string{"Corefile": ".:53 {\n    kubernetes corp.example in-addr.arpa ip6.arpa\n}\n"},
	}

	t.Run("Given CoreDNS serving a custom zone, when detecting the domain and planning an install, assert that the kagent chart receives that cluster domain", func(t *testing.T) {
		cfg := clusterDomainTestConfig(t)
		values := planAndRenderKagentValues(t, cfg, coreDNS)
		require.Equal(t, map[string]any{"clusterDomain": "corp.example"}, values["global"])
	})

	t.Run("Given CoreDNS serving a custom zone, when detecting the domain and planning with a configured values file that sets the domain, assert that the caller domain wins", func(t *testing.T) {
		cfg := clusterDomainTestConfig(t)
		file := filepath.Join(files, "domain.yaml")
		require.NoError(t, os.WriteFile(file, []byte("global: {clusterDomain: file.example}\n"), 0o600))
		cfg.Kagent.ValuesFiles = []string{file}
		values := planAndRenderKagentValues(t, cfg, coreDNS)
		require.Equal(t, map[string]any{"clusterDomain": "file.example"}, values["global"])
	})

	t.Run("Given CoreDNS serving a custom zone, when detecting the domain and planning with a chart value that sets the domain, assert that the caller domain wins", func(t *testing.T) {
		cfg := clusterDomainTestConfig(t)
		cfg.Kagent.Values = []string{"global.clusterDomain=set.example"}
		values := planAndRenderKagentValues(t, cfg, coreDNS)
		require.Equal(t, map[string]any{"clusterDomain": "set.example"}, values["global"])
	})

	t.Run("Given no CoreDNS ConfigMap, when detecting the domain and planning an install, assert that the chart default domain is left untouched", func(t *testing.T) {
		cfg := clusterDomainTestConfig(t)
		values := planAndRenderKagentValues(t, cfg)
		require.NotContains(t, values, "global")
	})
}

func clusterDomainTestConfig(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "kagent")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: kagent\nversion: 1.0.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "templates", "values.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: values}\ndata:\n  values: {{ .Values | toJson | quote }}\n"), 0o600))
	cfg := testConfig()
	cfg.SkipDatabaseSetup = true
	cfg.Kagent.Repository = root
	cfg.Kagent.ValuesFiles = nil
	return cfg
}

// planAndRenderKagentValues plans with a recording runner, then renders the
// planned kagent release with real Helm, so values precedence is Helm's own.
func planAndRenderKagentValues(t *testing.T, cfg Config, objects ...runtime.Object) map[string]any {
	t.Helper()
	client := NewClient(&recordingRunner{}, fake.NewSimpleClientset(objects...))
	domain, err := client.DetectClusterDomain(t.Context())
	require.NoError(t, err)
	cfg.ClusterDomain = domain
	plan, err := client.Plan(t.Context(), cfg)
	require.NoError(t, err)
	for _, step := range plan.Steps() {
		release, ok := step.(ChartInstall)
		if !ok || release.Release != kagentRelease {
			continue
		}
		command := Command{Program: "helm", Args: []string{"template", release.Release, chartReference(release.Chart), "--namespace", release.Namespace, "--show-only", "templates/values.yaml"}, Stdin: release.Chart.inlineValues, SecretValues: release.Chart.secretValues}
		command.Args = append(command.Args, helmValueArgs(release.Chart)...)
		out, err := CommandRunner{}.Run(t.Context(), command)
		require.NoError(t, err)
		var manifest struct{ Data map[string]string }
		require.NoError(t, yaml.Unmarshal([]byte(out), &manifest))
		var values map[string]any
		require.NoError(t, json.Unmarshal([]byte(manifest.Data["values"]), &values))
		return values
	}
	t.Fatal("plan has no kagent release")
	return nil
}
