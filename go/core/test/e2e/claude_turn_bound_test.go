package e2e_test

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// A Claude turn that outlives the Harness's KAGENT_TURN_TIMEOUT completes
// with the cap result instead of running on: its model call never answers.
func TestClaudeTurnEndsAtTheHarnessTimeLimit(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := cloneHarness(t, kube, claudeE2EHarness, "claude-time-limit-", func(harness *v1alpha3.Harness) {
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: "KAGENT_TURN_TIMEOUT", Value: "20s"})
	})
	modelURL, _ := startBlockingMockServer(t, map[string]any{})
	model := createClaudeMockModel(t, kube, reachableServerURL(t, modelURL, ""))
	template := createClaudeBoundTemplate(t, kube, harness.Name, model.Name, nil)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template)

	_, _, task := fixture.send(t, "Say hello.")
	requireStoppedAt(t, task, "turn_time_limit")
}

// The acceptance case of the caller-bearer bound: with KAGENT_PROPAGATE_TOKEN
// a turn that outlives the caller's bearer ends with the cap result before
// the bearer expires, not with the MCP server's 401.
func TestClaudeTurnEndsBeforeTheCallersBearerExpires(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	harness := cloneHarness(t, kube, claudeE2EHarness, "claude-bearer-exp-", func(harness *v1alpha3.Harness) {
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: "KAGENT_PROPAGATE_TOKEN", Value: "true"})
	})
	mcpURL, _ := startMCPMock(t)
	mcpServer := createClaudeMCPServer(t, kube, mcpURL)
	modelURL, _ := startBlockingMockServer(t, map[string]any{})
	model := createClaudeMockModel(t, kube, reachableServerURL(t, modelURL, ""))
	template := createClaudeBoundTemplate(t, kube, harness.Name, model.Name, []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
		Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: mcpServer.Name},
	}}})
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template)
	// The harness ends the turn 30s before exp; the minute after that is the
	// actor's start and the turn's first model call.
	expiry := time.Now().Add(90 * time.Second)
	fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, "authorization", unsignedBearer(expiry))

	_, _, task := fixture.send(t, "Say hello.")
	requireStoppedAt(t, task, "caller_credential_expiry")
	require.True(t, time.Now().Before(expiry), "the turn outlived the caller's bearer")
}

func createClaudeBoundTemplate(t *testing.T, kube ctrlclient.Client, harness, model string, tools []v1alpha3.ToolBinding) string {
	t.Helper()
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "claude-bound-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "claude"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: model},
			Description: "Claude turn bound E2E fixture", SystemPrompt: "Reply concisely.",
			Tools: tools,
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness)
	return template.Name
}

func requireStoppedAt(t *testing.T, task *a2atype.Task, reason string) {
	t.Helper()
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "text = %s", taskText(task))
	require.NotNil(t, task.Status.Message, "a turn stopped at a bound carries the cap result")
	require.Equal(t, reason, task.Status.Message.Metadata[apia2a.StopReasonMetadataKey])
	require.Contains(t, taskText(task), "continue")
}

// unsignedBearer is a JWT bearer that expires at expiry. The harness reads exp
// only; nothing in this test verifies the signature.
func unsignedBearer(expiry time.Time) string {
	encode := base64.RawURLEncoding.EncodeToString
	claims := fmt.Sprintf(`{"sub":"e2e-person","exp":%d}`, expiry.Unix())
	return "Bearer " + encode([]byte(`{"alg":"none"}`)) + "." + encode([]byte(claims)) + ".e2e"
}
