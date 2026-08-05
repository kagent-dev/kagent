package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestE2ETopologySpreadConstraints asserts a declarative agent's
// topologySpreadConstraints reach the rendered Deployment pod template spec
// unchanged.
func TestE2ETopologySpreadConstraints(t *testing.T) {
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)

	tsc := []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           1,
			TopologyKey:       "kubernetes.io/hostname",
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app.kubernetes.io/instance": "test-tsc"},
			},
		},
	}

	agent := setupAgentWithOptions(t, cli, modelCfg.Name, nil, AgentOptions{
		Name:                      "test-tsc",
		TopologySpreadConstraints: tsc,
	})

	deployment := &appsv1.Deployment{}
	require.NoError(t, cli.Get(t.Context(), client.ObjectKey{Name: agent.Name, Namespace: agent.Namespace}, deployment))
	require.Equal(t, tsc, deployment.Spec.Template.Spec.TopologySpreadConstraints,
		"topologySpreadConstraints from the agent spec should land on the rendered Deployment")
}
