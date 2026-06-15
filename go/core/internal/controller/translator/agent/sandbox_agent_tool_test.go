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

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

// Test_AdkApiTranslator_SandboxAgentTool tests that agent tool references are
// resolved by Kind, so both Agent and SandboxAgent objects can be used as tools.
func Test_AdkApiTranslator_SandboxAgentTool(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	declarativeSpec := func(tools ...*v1alpha2.Tool) v1alpha2.AgentSpec {
		return v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "test agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test",
				ModelConfig:   "default-model",
				Tools:         tools,
			},
		}
	}

	agentToolRef := func(name, kind string) *v1alpha2.Tool {
		return &v1alpha2.Tool{
			Type: v1alpha2.ToolProviderType_Agent,
			Agent: &v1alpha2.TypedReference{
				Name: name,
				Kind: kind,
			},
		}
	}

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model", Namespace: "test"},
		Spec: v1alpha2.ModelConfigSpec{
			Provider: "OpenAI",
			Model:    "gpt-4o",
		},
	}
	testNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	// A SandboxAgent and a regular Agent sharing the same name, to verify
	// kind-based resolution and kind-qualified self-reference checks.
	sandboxTool := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "shared-name", Namespace: "test"},
		Spec:       v1alpha2.SandboxAgentSpec{AgentSpec: declarativeSpec()},
	}
	regularTool := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "shared-name", Namespace: "test"},
		Spec:       declarativeSpec(),
	}

	tests := []struct {
		name        string
		agent       v1alpha2.AgentObject
		wantURL     string
		wantErr     bool
		errContains string
	}{
		{
			name: "kind SandboxAgent resolves SandboxAgent and routes via controller proxy",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "SandboxAgent")),
			},
			wantURL: "http://kagent-controller.kagent:8083/api/a2a-sandboxes/test/shared-name",
		},
		{
			name: "empty kind defaults to Agent",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "")),
			},
			wantURL: "http://shared-name.test:8080",
		},
		{
			name: "kind Agent resolves Agent and uses direct service URL",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "Agent")),
			},
			wantURL: "http://shared-name.test:8080",
		},
		{
			name: "unsupported kind is rejected",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "AgentHarness")),
			},
			wantErr:     true,
			errContains: `unsupported agent tool kind "AgentHarness"`,
		},
		{
			name: "missing SandboxAgent returns not found",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("does-not-exist", "SandboxAgent")),
			},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "SandboxAgent referencing itself is rejected",
			agent: &v1alpha2.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "shared-name", Namespace: "test"},
				Spec: v1alpha2.SandboxAgentSpec{
					AgentSpec: declarativeSpec(agentToolRef("shared-name", "SandboxAgent")),
				},
			},
			wantErr:     true,
			errContains: "reference itself",
		},
		{
			name: "Agent referencing a SandboxAgent with the same name is not a self-reference",
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "shared-name", Namespace: "test"},
				Spec:       declarativeSpec(agentToolRef("shared-name", "SandboxAgent")),
			},
			wantURL: "http://kagent-controller.kagent:8083/api/a2a-sandboxes/test/shared-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, testNamespace, sandboxTool, regularTool).
				Build()

			translator := agenttranslator.NewAdkApiTranslator(
				kubeClient,
				types.NamespacedName{Name: "default-model", Namespace: "test"},
				nil,
				"",
				nil,
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
