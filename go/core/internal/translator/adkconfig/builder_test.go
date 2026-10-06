package adkconfig

import (
	"context"
	"encoding/json"
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

func TestBuildModelStreaming(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream *bool
		want   bool
	}{
		{name: "default", want: true},
		{name: "enabled", stream: new(true), want: true},
		{name: "disabled", stream: new(false), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := openAIModel("agent", "https://agent.example.com/v1")
			model.Spec.Stream = tc.stream
			collections := contextTestCollections(t, model)
			result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(t.Context(), &v2translator.HarnessInput{
				Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
				Root: &v2translator.AgentInput{
					Template: &v2translator.TemplateConfiguration{}, ResolvedModelConfig: resolvedModel(t, collections, "agent"),
				},
			})
			require.NoError(t, err)
			// The runtime consumes serialized configuration. In particular, false
			// must survive omitempty rather than reverting to the runtime default.
			payload, err := json.Marshal(result.Config)
			require.NoError(t, err)
			var config adk.AgentConfig
			require.NoError(t, json.Unmarshal(payload, &config))
			require.NotNil(t, config.Stream)
			require.Equal(t, tc.want, config.GetStream())
		})
	}
}

func TestBuildUsesDurableSessionStore(t *testing.T) {
	result, err := NewBuilder(krt.TestingDummyContext{}, v2translator.Collections{}).Build(context.Background(),
		&v2translator.HarnessInput{
			Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{BYO: &v1alpha3.BYOHarness{}}},
			Root: &v2translator.AgentInput{Template: &v2translator.TemplateConfiguration{}, Shared: []v2translator.AgentInputBinding{{
				Name: "child", Agent: &v2translator.AgentInput{Template: &v2translator.TemplateConfiguration{}},
			}}},
		})
	require.NoError(t, err)
	require.True(t, result.Config.GetStream())
	require.Equal(t, "sqlite+aiosqlite:////data/sessions.db", result.Config.SessionDBURL)
	require.Len(t, result.Config.SubAgents, 1)
	require.Empty(t, result.Config.SubAgents[0].SessionDBURL)
}

func TestBuildCompaction(t *testing.T) {
	collections := contextTestCollections(t,
		openAIModel("agent", "https://agent.example.com/v1"),
		openAIModel("summarizer", "https://summarizer.example.com/v1"),
	)
	agentModel := resolvedModel(t, collections, "agent")
	template := &v2translator.TemplateConfiguration{
		Name: "assistant", Namespace: "test", Source: &metav1.ObjectMeta{Name: "assistant", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "agent"}},
	}
	harness := func(compaction *v1alpha3.KagentHarnessCompaction) *v2translator.HarnessConfiguration {
		return &v2translator.HarnessConfiguration{
			Name: "kagent", Namespace: "test", Source: &metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
			Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{Compaction: compaction}},
		}
	}
	apply := func(harness *v2translator.HarnessConfiguration) (*Result, error) {
		builder := NewBuilder(krt.TestingDummyContext{}, collections)
		return builder.Build(context.Background(), &v2translator.HarnessInput{
			Harness: harness, Root: &v2translator.AgentInput{Template: template, ResolvedModelConfig: agentModel},
		})
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
		require.Contains(t, result.Egress, "https://summarizer.example.com:443")
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

func TestMistralEgressDestination(t *testing.T) {
	for _, test := range []struct {
		name    string
		mistral *v1alpha3.MistralConfig
		want    string
	}{
		{name: "default endpoint", want: "https://api.mistral.ai:443"},
		{name: "base URL override", mistral: &v1alpha3.MistralConfig{BaseURL: new("https://mistral.example.com/v1")}, want: "https://mistral.example.com:443"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &v1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "mistral", Namespace: "test"},
				Spec: v1alpha3.ModelConfigSpec{
					Provider: v1alpha3.ModelProviderMistral, Model: "mistral-large-latest", Mistral: test.mistral,
				},
			}
			collections := contextTestCollections(t, model)
			result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(context.Background(),
				&v2translator.HarnessInput{
					Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
					Root: &v2translator.AgentInput{
						Template:            &v2translator.TemplateConfiguration{Name: "pi", Namespace: "test"},
						ResolvedModelConfig: resolvedModel(t, collections, "mistral"),
					}})
			require.NoError(t, err)
			require.Equal(t, []string{test.want}, result.Egress)
		})
	}
}

// TestOllamaEgressDestination covers the one provider whose endpoint is not
// reachable through the serialized model: it lives on the ModelConfig's own
// Ollama field, so the generic walk over the model never sees it. Without an
// explicit case the allowlist came back empty, which compiles into a deny-all
// Actor egress policy and fails the model call with a 403.
func TestOllamaEgressDestination(t *testing.T) {
	for _, host := range []string{"http://host.docker.internal:11434", "host.docker.internal:11434"} {
		t.Run(host, func(t *testing.T) {
			model := &v1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "test"},
				Spec: v1alpha3.ModelConfigSpec{
					Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
					Ollama: &v1alpha3.OllamaConfig{Host: host},
				},
			}
			collections := contextTestCollections(t, model)
			result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(context.Background(),
				&v2translator.HarnessInput{
					Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
					Root: &v2translator.AgentInput{
						Template:            &v2translator.TemplateConfiguration{Name: "pi", Namespace: "test"},
						ResolvedModelConfig: resolvedModel(t, collections, "ollama"),
					}})
			require.NoError(t, err)
			require.Equal(t, []string{"http://host.docker.internal:11434"}, result.Egress)
		})
	}

	t.Run("no host configured", func(t *testing.T) {
		model := &v1alpha3.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "test"},
			Spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				Ollama: &v1alpha3.OllamaConfig{},
			},
		}
		collections := contextTestCollections(t, model)
		result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(context.Background(),
			&v2translator.HarnessInput{
				Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
				Root: &v2translator.AgentInput{
					Template:            &v2translator.TemplateConfiguration{Name: "pi", Namespace: "test"},
					ResolvedModelConfig: resolvedModel(t, collections, "ollama"),
				}})
		require.NoError(t, err)
		require.Empty(t, result.Egress)
	})

	// A cloud-tagged model with a key bypasses the operator's host, so the cloud
	// host has to be allowed as well or egress policy denies the call.
	t.Run("cloud model with a secret allows api.ollama.com", func(t *testing.T) {
		model := &v1alpha3.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "test"},
			Spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "deepseek-v4-flash:0731-cloud",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{},
			},
		}
		collections := contextTestCollections(t, model)
		result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(context.Background(),
			&v2translator.HarnessInput{
				Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
				Root: &v2translator.AgentInput{
					Template:            &v2translator.TemplateConfiguration{Name: "pi", Namespace: "test"},
					ResolvedModelConfig: resolvedModel(t, collections, "ollama"),
				}})
		require.NoError(t, err)
		require.Contains(t, result.Egress, "https://api.ollama.com:443")
	})

	// A local model must never pick up the cloud host, even with a key present:
	// that is the rerouting regression the routing rules exist to prevent.
	t.Run("local model with a key does not allow the cloud host", func(t *testing.T) {
		model := &v1alpha3.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "test"},
			Spec: v1alpha3.ModelConfigSpec{
				Provider: v1alpha3.ModelProviderOllama, Model: "llama3.2",
				APIKeySecret: "ollama-cloud", APIKeySecretKey: "OLLAMA_API_KEY",
				Ollama: &v1alpha3.OllamaConfig{Host: "host.docker.internal:11434"},
			},
		}
		collections := contextTestCollections(t, model)
		result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(context.Background(),
			&v2translator.HarnessInput{
				Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
				Root: &v2translator.AgentInput{
					Template:            &v2translator.TemplateConfiguration{Name: "pi", Namespace: "test"},
					ResolvedModelConfig: resolvedModel(t, collections, "ollama"),
				}})
		require.NoError(t, err)
		require.Equal(t, []string{"http://host.docker.internal:11434"}, result.Egress)
	})
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

func TestBuildMemory(t *testing.T) {
	collections := contextTestCollections(t,
		openAIModel("agent", "https://agent.example.com/v1"),
		openAIModel("embedding", "https://embedding.example.com/v1"),
	)
	root := &v2translator.AgentInput{
		Template:            &v2translator.TemplateConfiguration{Name: "assistant", Namespace: "test"},
		ResolvedModelConfig: resolvedModel(t, collections, "agent"),
	}
	for _, tc := range []struct {
		name      string
		modelName string
		wantErr   string
	}{
		{name: "configured", modelName: "embedding"},
		{name: "missing model", modelName: "missing", wantErr: `resolve memory ModelConfig "missing"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			harness := &v2translator.HarnessConfiguration{
				Namespace: "test",
				Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{Memory: &v1alpha3.KagentHarnessMemory{
					ModelConfigRef: corev1.LocalObjectReference{Name: tc.modelName}, TTLDays: 7,
				}}},
			}
			result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(t.Context(), &v2translator.HarnessInput{Harness: harness, Root: root})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 7, result.Config.Memory.TTLDays)
			require.Equal(t, "https://embedding.example.com/v1", result.Config.Memory.Embedding.BaseUrl)
			require.Len(t, result.Models, 2)
			require.Equal(t, "embedding", result.Models[1].Config.Name)
			require.Contains(t, result.Egress, "https://embedding.example.com:443")
			require.Len(t, result.Environment, 2)
			require.Equal(t, "embedding-auth", result.Environment[1].ValueFrom.SecretKeyRef.Name)
		})
	}
}

func TestBuildModelRequirements(t *testing.T) {
	collections := contextTestCollections(t, openAIModel("agent", "https://agent.example.com/v1"))
	for _, tc := range []struct {
		name       string
		byo        bool
		rootModel  bool
		childModel bool
		wantErr    bool
	}{
		{name: "kagent missing root model", childModel: true, wantErr: true},
		{name: "kagent missing child model", rootModel: true, wantErr: true},
		{name: "kagent all models", rootModel: true, childModel: true},
		{name: "BYO without models", byo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			harness := &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}}
			if tc.byo {
				harness.Spec.Kagent, harness.Spec.BYO = nil, &v1alpha3.BYOHarness{}
			}
			child := &v2translator.AgentInput{Template: &v2translator.TemplateConfiguration{}}
			root := &v2translator.AgentInput{Template: &v2translator.TemplateConfiguration{}, Shared: []v2translator.AgentInputBinding{{Name: "child", Agent: child}}}
			if tc.rootModel {
				root.ResolvedModelConfig = resolvedModel(t, collections, "agent")
			}
			if tc.childModel {
				child.ResolvedModelConfig = resolvedModel(t, collections, "agent")
			}
			result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(t.Context(), &v2translator.HarnessInput{Harness: harness, Root: root})
			if tc.wantErr {
				require.ErrorContains(t, err, "kagent ModelConfig is required")
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.Config.SubAgents, 1)
		})
	}
}
