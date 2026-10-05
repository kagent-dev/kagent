package e2e_test

import (
	"bytes"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	harnessa2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A Claude Harness with a turn cap ends a turn that reaches it as completed,
// names the limit on the status message and reports the usage it spent.
func TestClaudeTurnStopsAtItsTurnLimit(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := cloneHarness(t, kube, claudeE2EHarness, "claude-limits-", func(harness *v1alpha3.Harness) {
		harness.Spec.Claude.Limits = &v1alpha3.ClaudeHarnessLimits{MaxTurns: 1}
	})
	modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_builtin_tools.json"), "")
	model := createClaudeMockModel(t, kube, modelURL)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-limits-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: model.Name},
			Description: "Claude turn limit E2E fixture", SystemPrompt: "Use your tools and report what they return.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template.Name)

	_, _, task := fixture.send(t, "Create the requested file and read it back.")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "a turn stopped by its limit completes: %s", taskText(task))
	require.NotNil(t, task.Status.Message, "a limited turn carries a status message")
	require.Equal(t, runtime.LimitTurns, task.Status.Message.Metadata[harnessa2a.StoppedByMetadataKey], "metadata = %#v", task.Status.Message.Metadata)
	usage, ok := task.Status.Message.Metadata[apia2a.UsageMetadataKey].(map[string]any)
	require.True(t, ok, "usage metadata = %#v", task.Status.Message.Metadata[apia2a.UsageMetadataKey])
	require.Contains(t, usage, "numTurns")
	require.Contains(t, strings.ToLower(taskText(task)), "turn", "the completion names the limit: %s", taskText(task))
}

// With KAGENT_PROPAGATE_TOKEN the Claude harness calls an MCP server as the
// person whose A2A call runs the turn: the upstream sees the caller's bearer,
// not a static credential and not the loopback token.
func TestClaudeHarnessForwardsTheCallerCredential(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := cloneHarness(t, kube, claudeE2EHarness, "claude-caller-", func(harness *v1alpha3.Harness) {
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: "KAGENT_PROPAGATE_TOKEN", Value: "true"})
	})
	mcpURL, mcpMock := startMCPMock(t)
	mcpServer := createClaudeMCPServer(t, kube, mcpURL)
	toolName := "mcp__" + mcpServer.Name + "__add_numbers"
	model := createClaudeMockModel(t, kube, startClaudeResourceMockLLM(t, toolName))
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-caller-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
			Description:  "Claude caller credential E2E fixture",
			SystemPrompt: "Use the configured MCP tool. Do not calculate the answer yourself.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: mcpServer.Name},
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template.Name)
	const credential = "Bearer e2e-person-token"
	fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, "authorization", credential)

	streamed := sendStreaming(t, fixture, "Add 3 and 5 using the configured MCP server.")
	require.Equal(t, a2atype.TaskStateCompleted, streamed.state, "text = %q, failure = %q", streamed.text, streamed.failureText)
	require.Contains(t, streamed.text, "CLAUDE_MCP_DONE result is 8")

	var calls int
	for _, request := range mcpMock.Requests() {
		if !bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) {
			continue
		}
		calls++
		require.Equal(t, credential, request.Headers.Get("Authorization"), "the MCP server must see the caller's credential, not %q", request.Headers.Get("Authorization"))
	}
	require.NotZero(t, calls, "the mock MCP server received no tool call")
}
