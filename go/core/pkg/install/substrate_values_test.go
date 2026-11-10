package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

const testSubstrateDefaults = `rustfs:
  enabled: true
  accessKey: chart-access-key
  secretKey: chart-secret-key
atelet:
  storageBackend: s3
`

func TestAteletInheritsSubstrateValues(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "Given shared Substrate overrides, when planning an install, assert that Atelet inherits the effective values"
		if override {
			name = "Given shared and Atelet overrides, when planning an install, assert that Atelet-specific files override shared set values"
		}
		t.Run(name, func(t *testing.T) {
			cfg := localInstallConfig(t)
			shared := filepath.Join(t.TempDir(), "shared.yaml")
			require.NoError(t, os.WriteFile(shared, []byte(`atelet:
  extraArgs: [--localhost-registry-replacement=kind-registry:5000]
  resources: {limits: {memory: 2Gi}}
  imageCredentialProviderConfig: /etc/provider.yaml
  imageCredentialProviderBinDir: /opt/providers
image: {registry: mirror.example, repository: custom/substrate}
imagePullSecrets: [{name: registry-key}]
global: {imageRegistry: global-mirror.example}
otel: {endpoint: http://collector:4317}
preview: [PerTemplateSnapshotLocation, SharedFeature]
`), 0o600))
			cfg.Substrate.ValuesFiles = []string{shared}
			cfg.Substrate.Values = []string{"atelet.resources.limits.memory=3Gi", "otel.endpoint=http://set-collector:4317"}
			wantMemory, wantEndpoint := "3Gi", "http://set-collector:4317"
			if override {
				file := filepath.Join(t.TempDir(), "atelet.yaml")
				require.NoError(t, os.WriteFile(file, []byte("atelet: {resources: {limits: {memory: 4Gi}}}\notel: {endpoint: http://node-collector:4317}\n"), 0o600))
				cfg.Atelet.ValuesFiles = []string{file}
				wantMemory, wantEndpoint = "4Gi", "http://node-collector:4317"
			}
			plan, err := NewClient(CommandRunner{}, fake.NewSimpleClientset()).Plan(t.Context(), cfg)
			require.NoError(t, err)
			atelet := plan.Steps()[len(plan.Steps())-3].(ChartInstall)
			values := renderedValues(t, atelet.Chart)
			require.Contains(t, values, "atelet")
			node := values["atelet"].(map[string]any)
			require.Equal(t, []any{"--localhost-registry-replacement=kind-registry:5000"}, node["extraArgs"])
			require.Equal(t, wantMemory, node["resources"].(map[string]any)["limits"].(map[string]any)["memory"])
			require.Equal(t, "/etc/provider.yaml", node["imageCredentialProviderConfig"])
			require.Equal(t, "/opt/providers", node["imageCredentialProviderBinDir"])
			require.Equal(t, map[string]any{"registry": "mirror.example", "repository": "custom/substrate"}, values["image"])
			require.Equal(t, []any{map[string]any{"name": "registry-key"}}, values["imagePullSecrets"])
			require.Equal(t, map[string]any{"imageRegistry": "global-mirror.example"}, values["global"])
			require.Equal(t, map[string]any{"endpoint": wantEndpoint}, values["otel"])
			require.Equal(t, []any{"PerTemplateSnapshotLocation", "SharedFeature"}, values["preview"])
			require.NotContains(t, values, "rustfs", "unused chart defaults must not be copied to Atelet")
			output := filepath.Join(t.TempDir(), "export")
			_, err = Export(t.Context(), CommandRunner{}, cfg, ExportOptions{Directory: output, ReadmeTemplate: testReadmeTemplate, APIKeyEnvironment: "MODEL_KEY"})
			require.NoError(t, err)
			data, err := os.ReadFile(filepath.Join(output, "values", ateletRelease+".yaml"))
			require.NoError(t, err)
			var exported map[string]any
			require.NoError(t, yaml.Unmarshal(data, &exported))
			require.Equal(t, values, exported)
		})
	}
}

func TestRustFSUsesEffectiveSubstrateValues(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		values []string
		want   map[string][]byte
	}{
		{"Given chart defaults, when installing, assert that storage credentials come from the selected chart", nil, map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("chart-access-key"), "AWS_SECRET_ACCESS_KEY": []byte("chart-secret-key")}},
		{"Given custom RustFS keys, when installing, assert that storage credentials use the overrides", []string{"rustfs.accessKey=custom-key", "rustfs.secretKey=custom-secret"}, map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("custom-key"), "AWS_SECRET_ACCESS_KEY": []byte("custom-secret")}},
		{"Given RustFS disabled, when installing, assert that no development storage Secret is created", []string{"rustfs.enabled=false", "atelet.storageBackend=gcs"}, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := localInstallConfig(t)
			cfg.Substrate.Values = scenario.values
			kube := fake.NewSimpleClientset()
			client := NewClient(installPlanningRunner(t), kube)
			plan, err := client.Plan(t.Context(), cfg)
			require.NoError(t, err)
			require.Empty(t, kube.Actions())
			require.NoError(t, client.Apply(t.Context(), plan, nil))
			secrets, err := kube.CoreV1().Secrets(substrateNamespace).List(t.Context(), metav1.ListOptions{})
			require.NoError(t, err)
			var credentials map[string][]byte
			for _, secret := range secrets.Items {
				if secret.Name == rustFSCredentialsSecret {
					credentials = secret.Data
				}
			}
			require.Equal(t, scenario.want, credentials)
			values := renderedValues(t, plan.Steps()[len(plan.Steps())-3].(ChartInstall).Chart)
			store := values["objectStore"].(map[string]any)
			if scenario.want == nil {
				require.Equal(t, map[string]any{"backend": "gcs"}, store)
			} else {
				require.Equal(t, rustFSCredentialsSecret, store["credentialsSecret"])
			}
		})
	}
}

func TestRustFSCredentialMismatchStopsInstall(t *testing.T) {
	cfg := localInstallConfig(t)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: rustFSCredentialsSecret, Namespace: substrateNamespace}, Data: map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("existing-key"), "AWS_SECRET_ACCESS_KEY": []byte("existing-secret")}}
	kube := fake.NewSimpleClientset(secret)
	runner := installPlanningRunner(t)
	client := NewClient(runner, kube)
	plan, err := client.Plan(t.Context(), cfg)
	require.NoError(t, err)
	err = client.Apply(t.Context(), plan, nil)
	require.ErrorContains(t, err, "does not match")
	require.NotContains(t, err.Error(), "existing-secret")
	got, err := kube.CoreV1().Secrets(substrateNamespace).Get(t.Context(), secret.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, secret.Data, got.Data)
	for _, command := range runner.commands {
		if command.Program == "helm" && command.Args[0] == "upgrade" {
			require.NotContains(t, command.Args, ateletRelease)
			require.NotContains(t, command.Args, substrateRelease)
		}
	}
}

func TestRustFSCredentialsPreserveExistingSecret(t *testing.T) {
	// Given matching credentials with operator metadata, when installing twice,
	// assert that neither install changes the existing Secret.
	cfg := localInstallConfig(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: rustFSCredentialsSecret, Namespace: substrateNamespace, Annotations: map[string]string{"owner": "operator"}},
		Data:       map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("chart-access-key"), "AWS_SECRET_ACCESS_KEY": []byte("chart-secret-key"), "extra": []byte("preserved")},
	}
	kube := fake.NewSimpleClientset(secret)
	client := NewClient(installPlanningRunner(t), kube)
	plan, err := client.Plan(t.Context(), cfg)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, client.Apply(t.Context(), plan, nil))
		got, err := kube.CoreV1().Secrets(substrateNamespace).Get(t.Context(), secret.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, secret, got)
	}
}

func TestInvalidStorageValuesStopPlanning(t *testing.T) {
	// Given an invalid credential value, when planning an install, assert that
	// it fails without exposing the credential or touching Kubernetes.
	cfg := localInstallConfig(t)
	cfg.Substrate.Values = []string{"rustfs.accessKey.invalid=private-credential"}
	kube := fake.NewSimpleClientset()
	_, err := NewClient(CommandRunner{}, kube).Plan(t.Context(), cfg)
	require.ErrorContains(t, err, "failed to decode Substrate storage settings")
	require.NotContains(t, err.Error(), "private-credential")
	require.Empty(t, kube.Actions())
}

func localInstallConfig(t *testing.T) Config {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for values integration")
	}
	root := t.TempDir()
	cfg := testConfig()
	cfg.SkipDatabaseSetup = true
	for _, chart := range []*Chart{&cfg.Kagent, &cfg.KagentCRDs, &cfg.Substrate, &cfg.SubstrateCRDs, &cfg.PodCertificate, &cfg.Atelet} {
		chart.Repository, chart.Version, chart.ValuesFiles = root, "0.0.0", nil
		dir := filepath.Join(root, chart.Name)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: "+chart.Name+"\nversion: 0.0.0\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "substrate", "values.yaml"), []byte(testSubstrateDefaults), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "kagent", "templates", "controller-deployment.yaml"), []byte("kind: Deployment\nspec: {template: {spec: {serviceAccountName: kagent-controller}}}\n"), 0o600))
	return cfg
}

func installPlanningRunner(t *testing.T) *recordingRunner {
	t.Helper()
	return &recordingRunner{run: func(cmd Command) (string, error) {
		if cmd.Program == "helm" && (cmd.Args[0] == "template" || cmd.Args[0] == "show") {
			return CommandRunner{}.Run(t.Context(), cmd)
		}
		return "", nil
	}}
}

func renderedValues(t *testing.T, chart Chart) map[string]any {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, writeValuesChart(dir))
	values, err := mergeValues(t.Context(), CommandRunner{}, chart, dir)
	require.NoError(t, err)
	return values
}
