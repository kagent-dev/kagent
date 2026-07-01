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

// Test_AdkApiTranslator_RemoteURLAgentTool covers cross-cluster declarative A2A
// (kagent-enterprise#1853). An Agent tool reference that sets an explicit URL must
// compile into a RemoteAgentConfig pointing at that URL WITHOUT resolving a local
// Agent CR — the referenced agent lives in another cluster and is not present in
// this cluster's API server. Without a URL, the same reference fails with
// "not found", which is the bug this feature fixes.
func Test_AdkApiTranslator_RemoteURLAgentTool(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model", Namespace: "test"},
		Spec: v1alpha2.ModelConfigSpec{
			Provider: "OpenAI",
			Model:    "gpt-4o",
		},
	}
	testNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

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

	const remoteURL = "http://remote-agent.kagent.svc.cluster.local:8080"

	tests := []struct {
		name            string
		tool            *v1alpha2.Tool
		wantErr         bool
		errContains     string
		wantURL         string
		wantDescription string
	}{
		{
			name: "URL set - resolves to the remote endpoint without a local CR (#1853 fix)",
			tool: &v1alpha2.Tool{
				Type: v1alpha2.ToolProviderType_Agent,
				Agent: &v1alpha2.TypedReference{
					Name:        "remote-agent",
					URL:         remoteURL,
					Description: "Remote netops specialist in the west cluster",
				},
			},
			wantURL:         remoteURL,
			wantDescription: "Remote netops specialist in the west cluster",
		},
		{
			name: "no URL and no local CR - fails as before (demonstrates the bug)",
			tool: &v1alpha2.Tool{
				Type: v1alpha2.ToolProviderType_Agent,
				Agent: &v1alpha2.TypedReference{
					Name: "remote-agent",
				},
			},
			wantErr:     true,
			errContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: only the model config + namespace exist in this cluster. The
			// referenced "remote-agent" is intentionally NOT created here — it
			// lives in the peer cluster.
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, testNamespace).
				Build()

			translator := agenttranslator.NewAdkApiTranslator(
				kubeClient,
				types.NamespacedName{Name: "default-model", Namespace: "test"},
				nil,
				"",
				nil,
			)

			sourceAgent := &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "orchestrator", Namespace: "test"},
				Spec:       declarativeSpec(tt.tool),
			}

			inputs, err := translator.CompileAgent(ctx, sourceAgent)

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
			assert.Equal(t, tt.wantDescription, inputs.Config.RemoteAgents[0].Description)
		})
	}
}
