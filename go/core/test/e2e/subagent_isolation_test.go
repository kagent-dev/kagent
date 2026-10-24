package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// TestSubAgentIsolationValidation covers the controller's judgment of an
// Isolated subagent before any runtime exists. An Isolated subagent runs as a
// tool in a conversation of its own, so a runtime that cannot do that, and a
// subagent that needs a person to approve a tool call, are both rejected with
// Compatible=False. Neither case starts an Actor.
func TestSubAgentIsolationValidation(t *testing.T) {
	t.Parallel()
	interactionTarget(t) // skips without a cluster
	for _, tt := range []struct {
		name            string
		harness         string
		requireApproval bool
		want            string
	}{
		{
			name: "runtime without isolated subagents", harness: codexE2EHarness,
			want: `subagent "reviewer" is Isolated, which Harness runtime "codex" does not support`,
		},
		{
			name: "subagent that needs approval", harness: "kagent", requireApproval: true,
			want: `subagent "reviewer" is Isolated and cannot pause for human approval`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kube := interactionKubeClient(t)

			server := &v1alpha3.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "isolation-mcp-", Namespace: "kagent"},
				Spec: v1alpha3.RemoteMCPServerSpec{
					Description: "Subagent isolation E2E fixture",
					Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
					URL:         "http://127.0.0.1:1/mcp",
				},
			}
			require.NoError(t, kube.Create(t.Context(), server))
			deleteOnCleanup(t, kube, server)

			reviewer := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "isolation-reviewer-", Namespace: "kagent"},
				Spec: v1alpha3.AgentTemplateSpec{
					SystemPrompt: "Review the work.",
					Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
						Server:          corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name},
						RequireApproval: tt.requireApproval,
					}}},
				},
			}
			require.NoError(t, kube.Create(t.Context(), reviewer))
			deleteOnCleanup(t, kube, reviewer)

			root := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "isolation-root-", Namespace: "kagent"},
				Spec: v1alpha3.AgentTemplateSpec{
					SystemPrompt: "Coordinate.",
					Tools: []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{
						Name: "reviewer", Description: "Reviews work.", Isolation: v1alpha3.SubAgentIsolationIsolated,
						TemplateRef: &corev1.LocalObjectReference{Name: reviewer.Name},
					}}},
				},
			}
			require.NoError(t, kube.Create(t.Context(), root))
			deleteOnCleanup(t, kube, root)

			agent := &v1alpha3.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: root.Name, Namespace: root.Namespace},
				Spec: v1alpha3.AgentSpec{
					Template:   v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: root.Name}},
					HarnessRef: corev1.LocalObjectReference{Name: tt.harness},
				},
			}
			require.NoError(t, kube.Create(t.Context(), agent))
			deleteOnCleanup(t, kube, agent)

			compatible := waitForIncompatible(t, kube, agent)
			require.Contains(t, compatible.Message, tt.want)
			require.Empty(t, agent.Status.LatestSuccessfulRevision, "no revision is prepared for a rejected configuration")
		})
	}
}
