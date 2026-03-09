package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// skipIfNoCLITest skips CLI E2E tests unless CLI_TEST=1 is set.
// These tests require a running kagent cluster with deployed agents.
func skipIfNoCLITest(t *testing.T) {
	t.Helper()
	if os.Getenv("CLI_TEST") == "" {
		t.Skip("Skipping CLI E2E test: set CLI_TEST=1 to run (requires kagent cluster with agents)")
	}
}

// a2aURLForAgent returns the A2A endpoint URL for a given agent.
func a2aURLForAgent(agentName string) string {
	base := kagentBaseURL()
	return base + "/api/a2a/kagent/" + agentName + "/"
}

// invokeAgent sends a message to an agent via A2A and returns the response text.
func invokeAgent(t *testing.T, agentName, message string) string {
	t.Helper()

	url := a2aURLForAgent(agentName)
	t.Logf("Invoking agent %s at %s", agentName, url)

	client, err := a2aclient.NewA2AClient(url, a2aclient.WithTimeout(5*time.Minute))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := client.SendMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Kind:  protocol.KindMessage,
			Role:  protocol.MessageRoleUser,
			Parts: []protocol.Part{protocol.NewTextPart(message)},
		},
	})
	require.NoError(t, err, "SendMessage failed for agent %s", agentName)

	task, ok := result.Result.(*protocol.Task)
	require.True(t, ok, "expected Task result, got %T", result.Result)

	// Extract text from history (agent messages).
	var texts []string
	for _, msg := range task.History {
		if msg.Role == protocol.MessageRoleAgent {
			for _, part := range msg.Parts {
				if tp, ok := part.(*protocol.TextPart); ok {
					texts = append(texts, tp.Text)
				}
			}
		}
	}

	// Also check the status message.
	if task.Status.Message != nil {
		for _, part := range task.Status.Message.Parts {
			if tp, ok := part.(*protocol.TextPart); ok {
				texts = append(texts, tp.Text)
			}
		}
	}

	return strings.Join(texts, " ")
}

// TestE2ECLIInvokeIstioAgentVersion invokes the istio-agent and verifies the
// response contains Istio version information.
func TestE2ECLIInvokeIstioAgentVersion(t *testing.T) {
	skipIfNoCLITest(t)

	text := invokeAgent(t, "istio-agent", "What version of Istio is installed in the cluster?")
	t.Logf("Response text: %s", text)

	lowerText := strings.ToLower(text)
	assert.True(t,
		strings.Contains(lowerText, "istio") || strings.Contains(lowerText, "version"),
		"Response should mention Istio or version, got: %s", text,
	)
}

// TestE2ECLIInvokeAgents is a table-driven test for invoking agents via the A2A
// protocol and evaluating responses against expected content.
func TestE2ECLIInvokeAgents(t *testing.T) {
	skipIfNoCLITest(t)

	tests := []struct {
		name           string
		agent          string
		task           string
		expectContains []string // response should contain at least one (case-insensitive)
	}{
		{
			name:           "istio_version",
			agent:          "istio-agent",
			task:           "What version of Istio is installed in the cluster?",
			expectContains: []string{"istio", "version", "1."},
		},
		{
			name:           "istio_namespace",
			agent:          "istio-agent",
			task:           "What namespace is Istio installed in?",
			expectContains: []string{"istio-system", "istio", "namespace"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := invokeAgent(t, tt.agent, tt.task)
			t.Logf("Response: %s", text)

			lowerText := strings.ToLower(text)
			found := false
			for _, expected := range tt.expectContains {
				if strings.Contains(lowerText, strings.ToLower(expected)) {
					found = true
					break
				}
			}
			assert.True(t, found,
				"Response should contain one of %v, got: %s", tt.expectContains, text,
			)
		})
	}
}

// TestE2ECLIBinaryInvoke smoke-tests the kagent CLI binary directly.
// Requires the `kagent` binary in PATH (or KAGENT_BIN env var).
func TestE2ECLIBinaryInvoke(t *testing.T) {
	skipIfNoCLITest(t)
	if os.Getenv("CLI_BINARY_TEST") == "" {
		t.Skip("Skipping CLI binary test: set CLI_BINARY_TEST=1 to run (requires kagent binary in PATH)")
	}

	bin := os.Getenv("KAGENT_BIN")
	if bin == "" {
		bin = "kagent"
	}

	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("kagent binary not found: %v", err)
	}

	args := []string{"invoke", "--agent", "istio-agent", "--task", "What version of Istio is installed?"}
	if url := os.Getenv("KAGENT_URL"); url != "" {
		args = append(args, "--kagent-url", url)
	}

	cmd := exec.CommandContext(t.Context(), bin, args...)
	out, err := cmd.CombinedOutput()
	t.Logf("kagent invoke output:\n%s", string(out))
	require.NoError(t, err, "kagent invoke failed")

	lowerOut := strings.ToLower(string(out))
	assert.True(t,
		strings.Contains(lowerOut, "istio") || strings.Contains(lowerOut, "version"),
		"CLI output should mention istio or version",
	)
}
