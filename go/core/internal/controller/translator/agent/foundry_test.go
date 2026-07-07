package agent

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func foundryModelConfig(name string) *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1-nano",
			Provider: v1alpha2.ModelProviderFoundry,
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   "https://example.cognitiveservices.azure.com/",
				Deployment: "gpt-4-1-nano",
				APIVersion: "2024-10-21",
			},
		},
	}
}

// TestTranslateModelFoundryWorkloadIdentity covers the implicit Workload
// Identity path: no apiKeySecret, so no FOUNDRY_API_KEY env var is mounted and
// the runtime falls back to DefaultAzureCredential.
func TestTranslateModelFoundryWorkloadIdentity(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := foundryModelConfig("foundry-model")
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig).Build()
	tr := &adkApiTranslator{kube: kubeClient}

	model, deploymentData, _, err := tr.translateModel(context.Background(), "default", "foundry-model")
	require.NoError(t, err)

	foundryModel, ok := model.(*adk.Foundry)
	require.True(t, ok)
	assert.Equal(t, "gpt-4.1-nano", foundryModel.Model)
	assert.Equal(t, "https://example.cognitiveservices.azure.com/", foundryModel.Endpoint)
	assert.Equal(t, "gpt-4-1-nano", foundryModel.Deployment)
	assert.Equal(t, "2024-10-21", foundryModel.APIVersion)

	assert.Equal(t, "https://example.cognitiveservices.azure.com/", envVarValue(t, deploymentData.EnvVars, env.FoundryEndpoint.Name()))
	assert.Equal(t, "gpt-4-1-nano", envVarValue(t, deploymentData.EnvVars, env.FoundryDeployment.Name()))
	assert.Equal(t, "2024-10-21", envVarValue(t, deploymentData.EnvVars, env.FoundryAPIVersion.Name()))
	assertNoEnvVar(t, deploymentData.EnvVars, env.FoundryAPIKey.Name())
}

// TestTranslateModelFoundryAPIKey covers the API-key path: apiKeySecret is set,
// so FOUNDRY_API_KEY is sourced from the referenced secret. A custom apiVersion
// is preserved.
func TestTranslateModelFoundryAPIKey(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := foundryModelConfig("foundry-model")
	modelConfig.Spec.APIKeySecret = "foundry-secret"
	modelConfig.Spec.APIKeySecretKey = "api-key"
	modelConfig.Spec.Foundry.APIVersion = "2025-01-01"
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig).Build()
	tr := &adkApiTranslator{kube: kubeClient}

	model, deploymentData, _, err := tr.translateModel(context.Background(), "default", "foundry-model")
	require.NoError(t, err)

	foundryModel, ok := model.(*adk.Foundry)
	require.True(t, ok)
	assert.Equal(t, "2025-01-01", foundryModel.APIVersion)

	apiKeyEnv := envVar(t, deploymentData.EnvVars, env.FoundryAPIKey.Name())
	require.NotNil(t, apiKeyEnv.ValueFrom)
	require.NotNil(t, apiKeyEnv.ValueFrom.SecretKeyRef)
	assert.Equal(t, "foundry-secret", apiKeyEnv.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "api-key", apiKeyEnv.ValueFrom.SecretKeyRef.Key)
	assert.Equal(t, "2025-01-01", envVarValue(t, deploymentData.EnvVars, env.FoundryAPIVersion.Name()))
}

// TestRequireFoundryGoRuntime verifies Foundry is gated to the Go runtime.
func TestRequireFoundryGoRuntime(t *testing.T) {
	tests := []struct {
		name      string
		modelType string
		runtime   v1alpha2.DeclarativeRuntime
		wantErr   bool
	}{
		{"foundry python rejected", adk.ModelTypeFoundry, v1alpha2.DeclarativeRuntime_Python, true},
		{"foundry go allowed", adk.ModelTypeFoundry, v1alpha2.DeclarativeRuntime_Go, false},
		{"non-foundry python allowed", adk.ModelTypeOpenAI, v1alpha2.DeclarativeRuntime_Python, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &v1alpha2.Agent{
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						Runtime:       tt.runtime,
						SystemMessage: "You are a test agent",
						ModelConfig:   "m",
					},
				},
			}
			err := requireFoundryGoRuntime(agent, tt.modelType)
			if tt.wantErr {
				require.ErrorContains(t, err, `Foundry model provider requires declarative runtime "go"`)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func envVar(t *testing.T, envVars []corev1.EnvVar, name string) corev1.EnvVar {
	t.Helper()
	for _, e := range envVars {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("env var %s not found", name)
	return corev1.EnvVar{}
}

func envVarValue(t *testing.T, envVars []corev1.EnvVar, name string) string {
	t.Helper()
	return envVar(t, envVars, name).Value
}

func assertNoEnvVar(t *testing.T, envVars []corev1.EnvVar, name string) {
	t.Helper()
	for _, e := range envVars {
		if e.Name == name {
			t.Fatalf("env var %s unexpectedly present", name)
		}
	}
}
