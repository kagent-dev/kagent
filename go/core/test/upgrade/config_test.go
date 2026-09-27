package upgrade

import (
	"os"
	"path/filepath"
	"testing"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
)

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
