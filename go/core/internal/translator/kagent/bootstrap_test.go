package kagent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/api/bootstrap"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestLargeADKConfigurationBootstrap(t *testing.T) {
	scenarios := []struct {
		name                              string
		rootBytes, children, minimumBytes int
	}{
		{name: "Given a 48 KiB ADK prompt, when its runtime template is built, assert that config bypasses the environment limit", rootBytes: 48 << 10, minimumBytes: 48 << 10},
		{name: "Given a 128 KiB ADK prompt, when its runtime template is built, assert that config remains intact", rootBytes: 128 << 10, minimumBytes: 128 << 10},
		{name: "Given three large shared ADK agents, when their runtime template is built, assert that a near-limit bundle remains intact", rootBytes: 4096, children: 3, minimumBytes: 245 << 10},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			input := largeADKInput(scenario.rootBytes, scenario.children)
			result, err := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, testRuntimeDefaults()).Compile(t.Context(), input)
			require.NoError(t, err)
			result.CPU, result.Memory = "1", "1Gi"
			id, err := result.Digest()
			require.NoError(t, err)
			bundle, err := result.BootstrapBundle(id)
			require.NoError(t, err)
			template, err := substrate.ActorTemplateForRevision(&result.Revision, id, bundle)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(bundle.ConfigJson)+len(bundle.AgentCardJson), scenario.minimumBytes)
			require.LessOrEqual(t, len(bundle.ConfigJson)+len(bundle.AgentCardJson), bootstrap.MaxPayloadBytes)
			var config configv1.ADKConfig
			require.NoError(t, runtimeconfig.Unmarshal(bundle.ConfigJson, &config))
			require.Equal(t, input.Root.Instruction, config.Agent.Instruction)
			require.Len(t, config.Agent.SubAgents, scenario.children)
			for i, child := range config.Agent.SubAgents {
				require.Equal(t, input.Root.Shared[i].Agent.Instruction, child.Instruction)
			}
			for _, variable := range template.Containers[0].Env {
				require.NotEqual(t, "KAGENT_CONFIG_JSON", variable.Name)
				require.NotEqual(t, "KAGENT_AGENT_CARD_JSON", variable.Name)
				require.LessOrEqual(t, len(variable.Value), 32768)
			}
		})
	}
}

func TestOversizedADKConfigurationRejected(t *testing.T) {
	// Given four 56 KiB shared agents, when compilation is prepared for runtime,
	// assert that the explicit bundle budget rejects it before compute creation.
	input := largeADKInput(4096, 4)
	result, err := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, testRuntimeDefaults()).Compile(t.Context(), input)
	require.NoError(t, err)
	require.Greater(t, len(result.ConfigJSON), bootstrap.MaxPayloadBytes)
	result.CPU, result.Memory = "1", "1Gi"
	id, err := result.Digest()
	require.NoError(t, err)
	bundle, err := result.BootstrapBundle(id)
	require.ErrorContains(t, err, "configuration and Agent Card exceed")
	require.Nil(t, bundle)
}

func largeADKInput(rootBytes, children int) *translator.HarnessInput {
	model := &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "model"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4.1-mini"}}}
	template := &translator.TemplateConfiguration{Name: "large", Namespace: "test", Source: &metav1.ObjectMeta{Name: "large", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{Description: strings.Repeat("d", 40<<10)}}
	root := &translator.AgentInput{Template: template, ResolvedModelConfig: model, Instruction: strings.Repeat("r", rootBytes)}
	for i := range children {
		root.Shared = append(root.Shared, translator.AgentInputBinding{Name: fmt.Sprintf("specialist_%d", i), Description: "Shared specialist", Agent: &translator.AgentInput{Template: &translator.TemplateConfiguration{Name: fmt.Sprintf("child_%d", i), Namespace: "test", Spec: v1alpha3.AgentTemplateSpec{Description: "Shared specialist"}}, ResolvedModelConfig: model, Instruction: strings.Repeat("c", 56<<10) + fmt.Sprintf("child-end-%d", i)}})
	}
	return &translator.HarnessInput{AgentName: "large", Root: root, Harness: &translator.HarnessConfiguration{Name: "adk", Namespace: "test", Source: metav1.ObjectMeta{Name: "adk", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}, Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}}}}}
}
