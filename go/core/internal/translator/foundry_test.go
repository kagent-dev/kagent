package translator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompileFoundryEndpoint(t *testing.T) {
	for _, location := range []string{"agent", "shared agent", "memory", "BYO"} {
		t.Run(location, func(t *testing.T) {
			model := &v1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "foundry", Namespace: "test"},
				Spec: v1alpha3.ModelConfigSpec{
					Model: "gpt-4o", Provider: v1alpha3.ModelProviderFoundry,
					Foundry: &v1alpha3.FoundryConfig{Deployment: "chat", APIVersion: "2024-10-21", EndpointFrom: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "account"}, Key: "endpoint",
					}}, Headers: []v1alpha3.Header{{Name: "api-key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "foundry-auth"}, Key: "token"}}}},
				},
			}
			original := model.DeepCopy()
			harness := &v1alpha3.Harness{
				ObjectMeta: metav1.ObjectMeta{Name: "harness", Namespace: "test"},
				Spec: v1alpha3.HarnessSpec{
					Kagent: &v1alpha3.KagentHarness{},

					Workload: v1alpha3.HarnessWorkload{Image: new("example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
					Substrate: v1alpha3.RuntimeSubstratePolicy{
						WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
					},
				},
			}
			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "test"},
				Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "help"},
			}
			objects := []any{model, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: model.Spec.Headers[0].ValueFrom.SecretKeyRef.Name, Namespace: model.Namespace},
				Data:       map[string][]byte{"token": []byte("secret-value")},
			}}
			switch location {
			case "shared agent":
				child := template.DeepCopy()
				child.Name = "child"
				template.Spec.ModelConfig.Name = "default-model"
				template.Spec.Tools = []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{
					Name: "child", Description: "delegate", TemplateRef: &corev1.LocalObjectReference{Name: child.Name},
				}}}
				objects = append(objects, child, modelConfig())
			case "memory":
				harness.Spec.Kagent.Memory = &v1alpha3.KagentHarnessMemory{ModelConfigRef: corev1.LocalObjectReference{Name: model.Name}}
				template.Spec.ModelConfig.Name = "default-model"
				objects = append(objects, modelConfig())
			case "BYO":
				harness.Spec.Kagent = nil
				harness.Spec.BYO = &v1alpha3.BYOHarness{}
			}
			objects = append(objects, harness)

			var previous translator.RevisionID
			for _, host := range []string{"first.services.ai.azure.com", "second.services.ai.azure.com"} {
				endpoint := "https://" + host
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "test"},
					Data:       map[string]string{"endpoint": endpoint},
				}
				revision, err := compiler(t, append(objects, configMap)...).CompileAgent(t.Context(), inlineAgent(harness, template))
				require.NoError(t, err)
				var config configv1.ADKConfig
				require.NoError(t, runtimeconfig.Unmarshal(revision.ConfigJSON, &config))
				runtimeModel := config.Agent.Model
				if location == "shared agent" {
					require.Len(t, config.Agent.SubAgents, 1)
					runtimeModel = config.Agent.SubAgents[0].Model
				}
				if location == "memory" {
					require.NotNil(t, config.Agent.Memory)
					require.NotNil(t, config.Agent.Memory.Embedding)
					require.Equal(t, endpoint, config.Agent.Memory.Embedding.Endpoint)
				} else {
					require.IsType(t, &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{}}}, runtimeModel)
					require.Equal(t, endpoint, runtimeModel.GetFoundry().Endpoint)
				}
				require.Contains(t, revision.Environment, corev1.EnvVar{Name: "FOUNDRY_ENDPOINT", Value: endpoint})
				require.Len(t, revision.Credentials, 1)
				require.Equal(t, endpoint+":443", revision.Credentials[0].Origin)
				require.Equal(t, "api-key", revision.Credentials[0].Header)
				require.Equal(t, "ate-secret://k8s.io/default/test/foundry-auth/token", revision.Credentials[0].URI)
				require.Contains(t, revision.EgressDestinations, "https://"+host+":443")
				require.Equal(t, original, model, "compilation must not patch the source ModelConfig")
				digest, err := revision.Digest()
				require.NoError(t, err)
				require.NotEqual(t, previous, digest, "endpoint changes must produce a new revision")
				previous = digest
			}
		})
	}
}
