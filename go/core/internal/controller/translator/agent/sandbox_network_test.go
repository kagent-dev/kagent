package agent_test

// --- UNCOMMENT 4: Sandbox network unit tests ---
//
// import (
// 	"context"
// 	"encoding/json"
// 	"testing"
//
// 	"github.com/stretchr/testify/assert"
// 	"github.com/stretchr/testify/require"
// 	appsv1 "k8s.io/api/apps/v1"
// 	corev1 "k8s.io/api/core/v1"
// 	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
// 	"k8s.io/apimachinery/pkg/types"
// 	schemev1 "k8s.io/client-go/kubernetes/scheme"
// 	"sigs.k8s.io/controller-runtime/pkg/client/fake"
//
// 	"github.com/kagent-dev/kagent/go/api/v1alpha2"
// 	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
// )
//
// func newTestAgent(sandboxNetwork *v1alpha2.SandboxNetworkConfig) *v1alpha2.Agent {
// 	return &v1alpha2.Agent{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      "test-agent",
// 			Namespace: "test",
// 		},
// 		Spec: v1alpha2.AgentSpec{
// 			Type: v1alpha2.AgentType_Declarative,
// 			Declarative: &v1alpha2.DeclarativeAgentSpec{
// 				SystemMessage: "Test agent",
// 				ModelConfig:   "test-model",
// 			},
// 			SandboxNetwork: sandboxNetwork,
// 		},
// 	}
// }
//
// func translateAgent(t *testing.T, agent *v1alpha2.Agent) *translator.AgentOutputs {
// 	t.Helper()
// 	ctx := context.Background()
//
// 	modelConfig := &v1alpha2.ModelConfig{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      "test-model",
// 			Namespace: "test",
// 		},
// 		Spec: v1alpha2.ModelConfigSpec{
// 			Provider: "OpenAI",
// 			Model:    "gpt-4o",
// 		},
// 	}
//
// 	scheme := schemev1.Scheme
// 	require.NoError(t, v1alpha2.AddToScheme(scheme))
//
// 	kubeClient := fake.NewClientBuilder().
// 		WithScheme(scheme).
// 		WithObjects(agent, modelConfig).
// 		Build()
//
// 	defaultModel := types.NamespacedName{
// 		Namespace: "test",
// 		Name:      "test-model",
// 	}
// 	tr := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")
//
// 	result, err := tr.TranslateAgent(ctx, agent)
// 	require.NoError(t, err)
// 	require.NotNil(t, result)
// 	return result
// }
//
// func findSecret(t *testing.T, result *translator.AgentOutputs) *corev1.Secret {
// 	t.Helper()
// 	for _, obj := range result.Manifest {
// 		if secret, ok := obj.(*corev1.Secret); ok {
// 			return secret
// 		}
// 	}
// 	t.Fatal("no Secret found in manifest")
// 	return nil
// }
//
// func findDeployment(t *testing.T, result *translator.AgentOutputs) *appsv1.Deployment {
// 	t.Helper()
// 	for _, obj := range result.Manifest {
// 		if dep, ok := obj.(*appsv1.Deployment); ok {
// 			return dep
// 		}
// 	}
// 	t.Fatal("no Deployment found in manifest")
// 	return nil
// }
//
// func findEnvVar(envs []corev1.EnvVar, name string) *corev1.EnvVar {
// 	for i := range envs {
// 		if envs[i].Name == name {
// 			return &envs[i]
// 		}
// 	}
// 	return nil
// }
//
// func TestSandboxNetwork_AllowedDomains(t *testing.T) {
// 	agent := newTestAgent(&v1alpha2.SandboxNetworkConfig{
// 		AllowedDomains: []string{"api.example.com", "*.github.com"},
// 	})
//
// 	result := translateAgent(t, agent)
// 	secret := findSecret(t, result)
//
// 	srtSettingsJSON, ok := secret.StringData["srt-settings.json"]
// 	require.True(t, ok, "srt-settings.json should be present in Secret")
//
// 	var settings map[string]any
// 	require.NoError(t, json.Unmarshal([]byte(srtSettingsJSON), &settings))
//
// 	network, ok := settings["network"].(map[string]any)
// 	require.True(t, ok, "settings should have network key")
//
// 	allowed, ok := network["allowedDomains"].([]any)
// 	require.True(t, ok, "network should have allowedDomains")
// 	assert.Equal(t, []any{"api.example.com", "*.github.com"}, allowed)
// }
//
// func TestSandboxNetwork_Nil(t *testing.T) {
// 	agent := newTestAgent(nil)
//
// 	result := translateAgent(t, agent)
// 	secret := findSecret(t, result)
//
// 	_, ok := secret.StringData["srt-settings.json"]
// 	assert.False(t, ok, "srt-settings.json should not be present when SandboxNetwork is nil")
// }
//
// func TestSandboxNetwork_EmptyLists(t *testing.T) {
// 	agent := newTestAgent(&v1alpha2.SandboxNetworkConfig{
// 		AllowedDomains: []string{},
// 	})
//
// 	result := translateAgent(t, agent)
// 	secret := findSecret(t, result)
//
// 	_, ok := secret.StringData["srt-settings.json"]
// 	assert.False(t, ok, "srt-settings.json should not be present when both domain lists are empty")
// }
//
// func TestSandboxNetwork_EnvVarSet(t *testing.T) {
// 	agent := newTestAgent(&v1alpha2.SandboxNetworkConfig{
// 		AllowedDomains: []string{"api.example.com"},
// 	})
//
// 	result := translateAgent(t, agent)
//
// 	// Find deployment and check env vars
// 	deployment := findDeployment(t, result)
// 	containers := deployment.Spec.Template.Spec.Containers
// 	require.Len(t, containers, 1)
//
// 	envVar := findEnvVar(containers[0].Env, "KAGENT_SRT_SETTINGS_PATH")
// 	require.NotNil(t, envVar, "KAGENT_SRT_SETTINGS_PATH env var should be set")
// 	assert.Equal(t, "/config/srt-settings.json", envVar.Value)
// }
//
// func TestSandboxNetwork_NoEnvVarWhenNil(t *testing.T) {
// 	agent := newTestAgent(nil)
//
// 	result := translateAgent(t, agent)
//
// 	deployment := findDeployment(t, result)
// 	containers := deployment.Spec.Template.Spec.Containers
// 	require.Len(t, containers, 1)
//
// 	envVar := findEnvVar(containers[0].Env, "KAGENT_SRT_SETTINGS_PATH")
// 	assert.Nil(t, envVar, "KAGENT_SRT_SETTINGS_PATH env var should not be set when SandboxNetwork is nil")
// }
