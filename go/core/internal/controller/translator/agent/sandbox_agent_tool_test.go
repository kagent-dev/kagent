package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

// Test_AdkApiTranslator_SandboxAgentTool tests that agent tools require and
// resolve SandboxAgent references.
func Test_AdkApiTranslator_SandboxAgentTool(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha3.AddToScheme(scheme))

	declarativeSpec := func(tools ...*v1alpha3.Tool) v1alpha3.AgentSpec {
		return v1alpha3.AgentSpec{
			Type:        v1alpha3.AgentType_Declarative,
			Description: "test agent",
			Declarative: &v1alpha3.DeclarativeAgentSpec{
				SystemMessage: "Test",
				ModelConfig:   "default-model",
				Tools:         tools,
			},
		}
	}

	agentToolRef := func(name, kind string) *v1alpha3.Tool {
		return &v1alpha3.Tool{
			Type: v1alpha3.ToolProviderType_Agent,
			Agent: &v1alpha3.TypedReference{
				Name: name,
				Kind: kind,
			},
		}
	}

	modelConfig := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model", Namespace: "test"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: "OpenAI",
			Model:    "gpt-4o",
		},
	}
	testNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	sandboxTool := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "shared-name", Namespace: "test"},
		Spec:       declarativeSpec(),
	}
	tests := []struct {
		name        string
		agent       *v1alpha3.SandboxAgent
		wantURL     string
		wantErr     bool
		errContains string
	}{
		{
			name: "kind SandboxAgent resolves SandboxAgent and routes via controller proxy",
			agent: &v1alpha3.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "SandboxAgent")),
			},
			wantURL: "http://kagent-controller.kagent:8083/api/a2a-sandboxes/test/shared-name",
		},
		{
			name: "empty kind is rejected",
			agent: &v1alpha3.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "")),
			},
			wantErr:     true,
			errContains: `unsupported agent tool kind ""`,
		},
		{
			name: "unsupported kind is rejected",
			agent: &v1alpha3.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "AgentHarness")),
			},
			wantErr:     true,
			errContains: `unsupported agent tool kind "AgentHarness"`,
		},
		{
			name: "missing SandboxAgent returns not found",
			agent: &v1alpha3.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("does-not-exist", "SandboxAgent")),
			},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "SandboxAgent referencing itself is rejected",
			agent: &v1alpha3.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "shared-name", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "SandboxAgent")),
			},
			wantErr:     true,
			errContains: "reference itself",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, testNamespace, sandboxTool).
				Build()

			translator := agenttranslator.NewAdkApiTranslator(
				kubeClient,
				types.NamespacedName{Name: "default-model", Namespace: "test"},
				nil,
				"",
				testSandboxBackend{},
			)

			inputs, err := translator.CompileAgent(ctx, tt.agent)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, inputs)
			require.NotNil(t, inputs.Config)
			require.Len(t, inputs.Config.RemoteAgents, 1)
			assert.Equal(t, tt.wantURL, inputs.Config.RemoteAgents[0].Url)
		})
	}
}
