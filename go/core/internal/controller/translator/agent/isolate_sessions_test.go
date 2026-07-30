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

// Test_AdkApiTranslator_IsolateSessions verifies the Tool.IsolateSessions flag is
// carried into the ADK RemoteAgentConfig the Go runtime consumes.
func Test_AdkApiTranslator_IsolateSessions(t *testing.T) {
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

	agentTool := func(isolate *bool) *v1alpha2.Tool {
		return &v1alpha2.Tool{
			Type:            v1alpha2.ToolProviderType_Agent,
			Agent:           &v1alpha2.TypedReference{Name: "specialist", Kind: "Agent"},
			IsolateSessions: isolate,
		}
	}

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model", Namespace: "test"},
		Spec:       v1alpha2.ModelConfigSpec{Provider: "OpenAI", Model: "gpt-4o"},
	}
	testNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	specialist := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "specialist", Namespace: "test"},
		Spec:       declarativeSpec(),
	}

	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name    string
		isolate *bool
		want    bool
	}{
		{name: "isolateSessions true", isolate: boolPtr(true), want: true},
		{name: "isolateSessions false", isolate: boolPtr(false), want: false},
		{name: "isolateSessions unset defaults to false", isolate: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"},
				Spec:       declarativeSpec(agentTool(tt.isolate)),
			}
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(modelConfig, testNamespace, specialist).
				Build()

			translator := agenttranslator.NewAdkApiTranslator(
				kubeClient,
				types.NamespacedName{Name: "default-model", Namespace: "test"},
				nil,
				"",
				nil,
			)

			inputs, err := translator.CompileAgent(ctx, parent)
			require.NoError(t, err)
			require.NotNil(t, inputs)
			require.NotNil(t, inputs.Config)
			require.Len(t, inputs.Config.RemoteAgents, 1)
			assert.Equal(t, tt.want, inputs.Config.RemoteAgents[0].IsolateSessions)
		})
	}
}
