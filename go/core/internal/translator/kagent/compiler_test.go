package kagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestCompilerAgentIDHeaderRequiresModelOptIn(t *testing.T) {
	const agentIDHeader = "X-Agent-Id"
	for _, test := range []struct {
		name      string
		agentName string
		enabled   *bool
		wantID    string
	}{
		{name: "omitted", agentName: "east"},
		{name: "explicit false", agentName: "east", enabled: new(false)},
		{name: "first Agent using shared template", agentName: "east", enabled: new(true), wantID: "team-a/east"},
		{name: "second Agent using shared template", agentName: "west", enabled: new(true), wantID: "team-a/west"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "shared-model", Namespace: "team-a"},
				Spec: v1alpha3.ModelConfigSpec{
					Provider:          v1alpha3.ModelProviderOpenAI,
					Model:             "gpt-4.1-mini",
					SendAgentIDHeader: test.enabled,
					DefaultHeaders:    map[string]string{"X-Tenant": "acme"},
				},
			}}
			harness := &translator.HarnessConfiguration{
				Name: "kagent", Namespace: "team-a", Source: metav1.ObjectMeta{Name: "kagent", Namespace: "team-a"},
				Spec: v1alpha3.HarnessSpec{
					Kagent: &v1alpha3.KagentHarness{},
					Substrate: v1alpha3.RuntimeSubstratePolicy{
						WorkerPoolRef: corev1.LocalObjectReference{Name: "default"},
						SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
					},
				},
			}
			template := &translator.TemplateConfiguration{
				Name: "shared-template", Namespace: "team-a",
				Source: &metav1.ObjectMeta{Name: "shared-template", Namespace: "team-a"},
			}
			result, err := NewCompiler(krt.TestingDummyContext{}, translator.Collections{}, testRuntimeDefaults()).Compile(context.Background(), &translator.HarnessInput{
				AgentName: test.agentName,
				Harness:   harness,
				Root:      &translator.AgentInput{Template: template, ResolvedModelConfig: model},
			})
			require.NoError(t, err)

			var config configv1.ADKConfig
			require.NoError(t, runtimeconfig.Unmarshal(result.ConfigJSON, &config))
			require.NotNil(t, config.Agent.Model)
			require.Equal(t, "acme", config.Agent.Model.Headers["X-Tenant"])
			if test.wantID == "" {
				require.NotContains(t, config.Agent.Model.Headers, agentIDHeader)
			} else {
				require.Equal(t, test.wantID, config.Agent.Model.Headers[agentIDHeader])
			}
		})
	}
}
