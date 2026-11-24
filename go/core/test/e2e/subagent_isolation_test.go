package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// TestSubAgentIsolationValidation covers the controller's judgment of a Session
// subagent before any runtime exists. A Session subagent runs as a tool in a
// session of its own, so a runtime that cannot do that, and a subagent that
// needs a person to approve a tool call, are both rejected with
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
			name:    "Given a Codex Harness, when an Agent binds a Session subagent, assert that the Agent is incompatible",
			harness: codexE2EHarness,
			want:    `subagent "reviewer" runs in its own Session, which Harness runtime "codex" does not support`,
		},
		{
			name:    "Given a kagent Harness, when an Agent binds a Session subagent whose MCP server requires approval, assert that the Agent is incompatible",
			harness: "kagent", requireApproval: true,
			want: `subagent "reviewer" runs in its own Session and cannot pause for human approval`,
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
						Server:          v1alpha3.MCPServerReference{Name: server.Name},
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
						Name: "reviewer", Description: "Reviews work.", Isolation: v1alpha3.SubAgentIsolationSession,
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

func deleteOnCleanup(t *testing.T, kube ctrlclient.Client, object ctrlclient.Object) {
	t.Helper()
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), object); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete %T %s/%s: %v", object, object.GetNamespace(), object.GetName(), err)
		}
	})
}

// waitForIncompatible waits for the controller to reject the Agent's current
// generation with Compatible=False for an unsupported configuration, and returns
// that condition.
func waitForIncompatible(t *testing.T, kube ctrlclient.Client, agent *v1alpha3.Agent) *metav1.Condition {
	t.Helper()
	var compatible *metav1.Condition
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(agent), agent); err != nil {
			return false, err
		}
		compatible = apimeta.FindStatusCondition(agent.Status.Conditions, v1alpha3.AgentConditionCompatible)
		return compatible != nil && compatible.ObservedGeneration == agent.Generation && compatible.Status == metav1.ConditionFalse, nil
	})
	require.NoError(t, err, "last conditions: %+v", agent.Status.Conditions)
	require.Equal(t, "UnsupportedConfiguration", compatible.Reason)
	return compatible
}
