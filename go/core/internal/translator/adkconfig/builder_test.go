package adkconfig

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyCompaction(t *testing.T) {
	collections := contextTestCollections(t,
		openAIModel("agent", "https://agent.example.com/v1"),
		openAIModel("summarizer", "https://summarizer.example.com/v1"),
	)
	agentModel := resolvedModel(t, collections, "agent")
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"},
		Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "agent"}},
	}
	harness := func(compaction *v1alpha3.KagentHarnessCompaction) *v1alpha3.Harness {
		return &v1alpha3.Harness{
			ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
			Spec:       v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{Compaction: compaction}},
		}
	}
	apply := func(harness *v1alpha3.Harness) (*Result, error) {
		builder := NewBuilder(krt.TestingDummyContext{}, collections)
		result, err := builder.Build(context.Background(), &v2translator.AgentInput{Template: template, ResolvedModelConfig: agentModel})
		require.NoError(t, err)
		return result, builder.ApplyCompaction(result, harness, template)
	}

	t.Run("not configured", func(t *testing.T) {
		result, err := apply(harness(nil))
		require.NoError(t, err)
		require.Nil(t, result.Config.ContextConfig)
	})

	t.Run("strategies and a prompt on the agent model", func(t *testing.T) {
		result, err := apply(harness(&v1alpha3.KagentHarnessCompaction{
			CompactionInterval: new(5), OverlapSize: new(2), TokenThreshold: new(50000), EventRetentionSize: new(10),
			Summarizer: &v1alpha3.KagentHarnessSummarizer{
				ModelConfigRef: &corev1.LocalObjectReference{Name: "agent"}, PromptTemplate: "Summarize.\n\n{conversation_history}",
			},
		}))
		require.NoError(t, err)
		// The agent's own model is the runtime's default summarizer, so it is
		// not repeated in the configuration.
		require.Equal(t, &adk.AgentContextConfig{Compaction: &adk.AgentCompressionConfig{
			CompactionInterval: new(5), OverlapSize: new(2), TokenThreshold: new(50000), EventRetentionSize: new(10),
			PromptTemplate: "Summarize.\n\n{conversation_history}",
		}}, result.Config.ContextConfig)
		require.Len(t, result.Models, 1)
	})

	t.Run("dedicated summarizer model", func(t *testing.T) {
		result, err := apply(harness(&v1alpha3.KagentHarnessCompaction{
			CompactionInterval: new(2),
			Summarizer:         &v1alpha3.KagentHarnessSummarizer{ModelConfigRef: &corev1.LocalObjectReference{Name: "summarizer"}},
		}))
		require.NoError(t, err)
		summarizer, ok := result.Config.ContextConfig.Compaction.SummarizerModel.(*adk.OpenAI)
		require.True(t, ok, "summarizer model = %T", result.Config.ContextConfig.Compaction.SummarizerModel)
		require.Equal(t, "https://summarizer.example.com/v1", summarizer.BaseUrl)
		require.Len(t, result.Models, 2)
		require.Equal(t, "summarizer", result.Models[1].Config.Name)
		require.Contains(t, result.Egress, "summarizer.example.com")
		var credentials []string
		for _, variable := range result.Environment {
			if variable.ValueFrom != nil && variable.ValueFrom.SecretKeyRef != nil {
				credentials = append(credentials, variable.ValueFrom.SecretKeyRef.Name)
			}
		}
		require.Equal(t, []string{"agent-auth", "summarizer-auth"}, credentials)
	})

	t.Run("missing summarizer model", func(t *testing.T) {
		_, err := apply(harness(&v1alpha3.KagentHarnessCompaction{
			CompactionInterval: new(2),
			Summarizer:         &v1alpha3.KagentHarnessSummarizer{ModelConfigRef: &corev1.LocalObjectReference{Name: "missing"}},
		}))
		require.ErrorContains(t, err, `summarizer ModelConfig "missing"`)
	})
}

func openAIModel(name, baseURL string) *v1alpha3.ModelConfig {
	return &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4.1-mini",
			APIKeySecret: name + "-auth", APIKeySecretKey: env.OpenAIAPIKey.Name(),
			OpenAI: &v1alpha3.OpenAIConfig{BaseURL: baseURL},
		},
	}
}

func contextTestCollections(t *testing.T, models ...*v1alpha3.ModelConfig) v2translator.Collections {
	t.Helper()
	objects := make([]any, 0, 2*len(models))
	for _, model := range models {
		objects = append(objects, model, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: model.Spec.APIKeySecret, Namespace: model.Namespace},
			Data:       map[string][]byte{model.Spec.APIKeySecretKey: []byte("secret")},
		})
	}
	mock := krttest.NewMock(t, objects)
	collections := v2translator.Collections{
		ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock),
		Secrets:    krttest.GetMockCollection[*corev1.Secret](mock),
	}
	// Every object given to the mock has to be consumed through a typed collection.
	modelConfigs := krttest.GetMockCollection[*v1alpha3.ModelConfig](mock)
	resolved := make([]any, 0, len(models))
	for _, model := range modelConfigs.List() {
		value, err := v2translator.ResolveModelConfig(krt.TestingDummyContext{}, collections, model)
		require.NoError(t, err)
		resolved = append(resolved, *value)
	}
	collections.ResolvedModelConfigs = krttest.GetMockCollection[v2translator.ResolvedModelConfig](krttest.NewMock(t, resolved))
	return collections
}

func resolvedModel(t *testing.T, collections v2translator.Collections, name string) *v2translator.ResolvedModelConfig {
	t.Helper()
	for _, resolved := range collections.ResolvedModelConfigs.List() {
		if resolved.Config.Name == name {
			return &resolved
		}
	}
	t.Fatalf("model %q not resolved", name)
	return nil
}
