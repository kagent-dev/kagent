package upgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

func TestInvokeHarnesses(t *testing.T) {
	manifest, err := os.ReadFile("../e2e/manifests/lifecycle.yaml.tmpl")
	require.NoError(t, err)
	for _, scenario := range []struct {
		name       string
		repository string
	}{
		{"Given a released controller, when preparing invocation fixtures, assert that they use its published runtimes", "ghcr.io/kagent-dev/kagent"},
		{"Given an upgraded controller, when preparing invocation fixtures, assert that they use its locally built runtimes", "localhost:5001/kagent-dev/kagent"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			images := &configv1.HarnessImagesConfig{
				Kagent: scenario.repository + "/golang-adk@sha256:" + strings.Repeat("a", 64),
				Claude: scenario.repository + "/claude-harness@sha256:" + strings.Repeat("b", 64),
				Codex:  scenario.repository + "/codex-harness@sha256:" + strings.Repeat("c", 64),
			}
			encoded, err := invokeHarnesses(manifest, images)
			require.NoError(t, err)
			var fixtures v1alpha3.HarnessList
			require.NoError(t, json.Unmarshal(encoded, &fixtures))
			require.Equal(t, "List", fixtures.Kind)
			require.Len(t, fixtures.Items, 4)
			want := map[string]string{
				"kagent": images.Kagent, "byo-adk-e2e": images.Kagent,
				"claude-e2e": images.Claude, "codex-e2e": images.Codex,
			}
			for _, fixture := range fixtures.Items {
				require.Equal(t, "Harness", fixture.Kind)
				require.Contains(t, want, fixture.Name)
				require.NotNil(t, fixture.Spec.Workload.Image)
				require.Equal(t, want[fixture.Name], *fixture.Spec.Workload.Image)
				require.Equal(t, "kagent-default", fixture.Spec.Substrate.WorkerPoolRef.Name)
				delete(want, fixture.Name)
			}
			require.Empty(t, want)
		})
	}
	t.Run("Given no runtime catalog, when preparing invocation fixtures, assert that setup reports the missing images", func(t *testing.T) {
		_, err := invokeHarnesses(manifest, nil)
		require.ErrorContains(t, err, "installed controller must provide kagent, claude, and codex runtime images")
	})
}

func TestLoadUpgradeEnv(t *testing.T) {
	repoRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "Makefile"), nil, 0o600))

	for _, tc := range []struct {
		name   string
		values map[string]string
		want   upgradeEnv
	}{
		{
			name: "empty settings use defaults",
			want: upgradeEnv{dockerRegistry: "localhost:5001", kindClusterName: "kagent", namespace: "kagent", kubeContext: "kind-kagent", openAIAPIKey: "fake"},
		},
		{
			name:   "context follows cluster name",
			values: map[string]string{kagentenv.E2EKindClusterName.Name(): "custom"},
			want:   upgradeEnv{dockerRegistry: "localhost:5001", kindClusterName: "custom", namespace: "kagent", kubeContext: "kind-custom", openAIAPIKey: "fake"},
		},
		{
			name: "explicit settings override defaults",
			values: map[string]string{
				kagentenv.E2EDockerRegistry.Name():  "registry.example",
				kagentenv.E2EKindClusterName.Name(): "custom",
				kagentenv.E2ENamespace.Name():       "agents",
				kagentenv.E2EKubeContext.Name():     "explicit-context",
				kagentenv.OpenAIAPIKey.Name():       "test-key",
			},
			want: upgradeEnv{dockerRegistry: "registry.example", kindClusterName: "custom", namespace: "agents", kubeContext: "explicit-context", openAIAPIKey: "test-key"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(kagentenv.E2ERepoRoot.Name(), repoRoot)
			t.Setenv(kagentenv.E2EUpgradeFromVersion.Name(), "1.0.0")
			t.Setenv(kagentenv.E2EVersion.Name(), "1.1.0")
			for _, variable := range []kagentenv.StringVar{kagentenv.E2EDockerRegistry, kagentenv.E2EKindClusterName, kagentenv.E2ENamespace, kagentenv.E2EKubeContext, kagentenv.OpenAIAPIKey} {
				t.Setenv(variable.Name(), tc.values[variable.Name()])
			}
			want := tc.want
			want.repoRoot = repoRoot
			want.upgradeFromVersion = "1.0.0"
			want.version = "1.1.0"
			require.Equal(t, want, loadUpgradeEnv(t))
		})
	}
}
