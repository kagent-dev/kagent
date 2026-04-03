package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// --- parseAllowedAgents unit tests ---

func TestParseAllowedAgents(t *testing.T) {
	tests := []struct {
		name      string
		agentsVal string // raw value of the "agents" query parameter (empty = omit the param)
		wantNil   bool
		wantRefs  []string // expected keys in the returned set
	}{
		{
			name:    "no agents parameter returns nil (all agents allowed)",
			wantNil: true,
		},
		{
			name:      "empty agents parameter returns nil",
			agentsVal: "",
			wantNil:   true,
		},
		{
			name:      "single agent ref",
			agentsVal: "kagent/k8s-agent",
			wantRefs:  []string{"kagent/k8s-agent"},
		},
		{
			name:      "multiple agent refs",
			agentsVal: "kagent/k8s-agent,kagent/helm-agent,kagent/observability-agent",
			wantRefs:  []string{"kagent/k8s-agent", "kagent/helm-agent", "kagent/observability-agent"},
		},
		{
			name:      "whitespace around refs is trimmed",
			agentsVal: " kagent/k8s-agent , kagent/helm-agent ",
			wantRefs:  []string{"kagent/k8s-agent", "kagent/helm-agent"},
		},
		{
			name:      "comma-only value returns nil",
			agentsVal: ",,,",
			wantNil:   true,
		},
		{
			name:      "duplicate refs are deduplicated",
			agentsVal: "kagent/k8s-agent,kagent/k8s-agent",
			wantRefs:  []string{"kagent/k8s-agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://example.com/mcp", nil)
			if tt.agentsVal != "" {
				q := url.Values{}
				q.Set("agents", tt.agentsVal)
				r.URL.RawQuery = q.Encode()
			}

			got := parseAllowedAgents(r)

			if tt.wantNil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.Len(t, got, len(tt.wantRefs))
			for _, ref := range tt.wantRefs {
				assert.Contains(t, got, ref, "expected ref %q in allow-list", ref)
			}
		})
	}
}

// --- allowedAgentsFromContext unit tests ---

func TestAllowedAgentsFromContext(t *testing.T) {
	t.Run("returns nil when no value in context", func(t *testing.T) {
		got := allowedAgentsFromContext(context.Background())
		assert.Nil(t, got)
	})

	t.Run("returns set stored in context", func(t *testing.T) {
		want := map[string]struct{}{"kagent/k8s-agent": {}}
		ctx := context.WithValue(context.Background(), allowedAgentsKey, want)
		got := allowedAgentsFromContext(ctx)
		assert.Equal(t, want, got)
	})
}

// --- MCP handler integration tests ---

// scheme holds the CRD types used in fake client construction.
var testScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	if err := v1alpha2.AddToScheme(s); err != nil {
		panic(fmt.Sprintf("failed to add v1alpha2 to scheme: %v", err))
	}
	return s
}()

// readyAgent returns an Agent object with both Accepted and DeploymentReady conditions.
func readyAgent(namespace, name, description string) *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.AgentSpec{
			Description: description,
		},
		Status: v1alpha2.AgentStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Accepted",
					Status: metav1.ConditionTrue,
				},
				{
					Type:   "Ready",
					Reason: "DeploymentReady",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}
}

// mcpSession holds an established MCP session for use across multiple calls.
type mcpSession struct {
	handler   http.Handler
	sessionID string
	targetURL string // base URL including any ?agents= query param
}

// newMCPSession performs the MCP initialize handshake with the given handler at
// the given URL (which may include query parameters such as ?agents=…) and
// returns a session ready for tool calls.
func newMCPSession(t *testing.T, handler http.Handler, targetURL string) *mcpSession {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      0,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "initialize must succeed")

	sessionID := rr.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID, "server must return Mcp-Session-Id after initialize")

	return &mcpSession{handler: handler, sessionID: sessionID, targetURL: targetURL}
}

// call sends a tools/call request within this session and returns the first
// "data:" event payload.
func (s *mcpSession) call(t *testing.T, toolName string, args any) map[string]any {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, s.targetURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", s.sessionID)

	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)

	// Parse the SSE stream and return the first data event.
	scanner := bufio.NewScanner(rr.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(after), &result))
			return result
		}
	}
	t.Fatalf("no data event found in MCP response (status %d, session %s): %s",
		rr.Code, s.sessionID, rr.Body.String())
	return nil
}

// extractToolResult returns the "result.content[0].text" field from an MCP
// tools/call response, and whether the result carries IsError=true.
func extractToolResult(t *testing.T, resp map[string]any) (text string, isError bool) {
	t.Helper()

	result, ok := resp["result"].(map[string]any)
	require.True(t, ok, "expected result field in response")

	isError, _ = result["isError"].(bool)

	content, ok := result["content"].([]any)
	require.True(t, ok, "expected content array in result")
	require.NotEmpty(t, content)

	first, ok := content[0].(map[string]any)
	require.True(t, ok)

	text, _ = first["text"].(string)
	return text, isError
}

// newTestHandler creates an MCPHandler backed by a fake Kubernetes client
// pre-populated with the given agents. The a2aBaseURL is left intentionally
// empty: tests that exercise invoke_agent for blocked agents never reach the
// A2A layer, so no real backend is needed.
func newTestHandler(t *testing.T, agents ...*v1alpha2.Agent) *MCPHandler {
	t.Helper()

	objs := make([]runtime.Object, len(agents))
	for i, a := range agents {
		objs[i] = a
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&v1alpha2.Agent{}).
		Build()

	handler, err := NewMCPHandler(fakeClient, "http://unused-a2a-base", nil)
	require.NoError(t, err)
	return handler
}

// TestListAgents_NoFilter verifies that without a filter all ready agents
// are returned.
func TestListAgents_NoFilter(t *testing.T) {
	handler := newTestHandler(t,
		readyAgent("kagent", "k8s-agent", "Kubernetes expert"),
		readyAgent("kagent", "helm-agent", "Helm expert"),
	)

	sess := newMCPSession(t, handler, "/mcp")
	resp := sess.call(t, "list_agents", map[string]any{})

	text, isError := extractToolResult(t, resp)
	assert.False(t, isError)
	assert.Contains(t, text, "kagent/k8s-agent")
	assert.Contains(t, text, "kagent/helm-agent")
}

// TestListAgents_WithFilter verifies that the allow-list restricts the
// list_agents response to only the permitted agents.
func TestListAgents_WithFilter(t *testing.T) {
	handler := newTestHandler(t,
		readyAgent("kagent", "k8s-agent", "Kubernetes expert"),
		readyAgent("kagent", "helm-agent", "Helm expert"),
		readyAgent("kagent", "observability-agent", "Observability expert"),
	)

	// Establish a session scoped to k8s-agent only.
	sess := newMCPSession(t, handler, "/mcp?agents=kagent%2Fk8s-agent")
	resp := sess.call(t, "list_agents", map[string]any{})

	text, isError := extractToolResult(t, resp)
	assert.False(t, isError)
	assert.Contains(t, text, "kagent/k8s-agent", "allowed agent must appear in result")
	assert.NotContains(t, text, "kagent/helm-agent", "non-allowed agent must not appear")
	assert.NotContains(t, text, "kagent/observability-agent", "non-allowed agent must not appear")
}

// TestListAgents_MultipleFilter verifies that a multi-agent allow-list permits
// exactly the specified agents and no others.
func TestListAgents_MultipleFilter(t *testing.T) {
	handler := newTestHandler(t,
		readyAgent("kagent", "k8s-agent", "Kubernetes expert"),
		readyAgent("kagent", "helm-agent", "Helm expert"),
		readyAgent("kagent", "observability-agent", "Observability expert"),
	)

	sess := newMCPSession(t, handler, "/mcp?agents=kagent%2Fk8s-agent,kagent%2Fhelm-agent")
	resp := sess.call(t, "list_agents", map[string]any{})

	text, isError := extractToolResult(t, resp)
	assert.False(t, isError)
	assert.Contains(t, text, "kagent/k8s-agent")
	assert.Contains(t, text, "kagent/helm-agent")
	assert.NotContains(t, text, "kagent/observability-agent")
}

// TestInvokeAgent_BlockedByFilter verifies that invoke_agent is rejected
// for agents outside the session allow-list, without touching the A2A layer.
func TestInvokeAgent_BlockedByFilter(t *testing.T) {
	handler := newTestHandler(t,
		readyAgent("kagent", "k8s-agent", "Kubernetes expert"),
		readyAgent("kagent", "helm-agent", "Helm expert"),
	)

	// Session is scoped to k8s-agent only — helm-agent must be rejected.
	sess := newMCPSession(t, handler, "/mcp?agents=kagent%2Fk8s-agent")
	resp := sess.call(t, "invoke_agent", map[string]any{
		"agent": "kagent/helm-agent",
		"task":  "List all releases",
	})

	text, isError := extractToolResult(t, resp)
	assert.True(t, isError, "response must carry IsError=true for a blocked agent")
	assert.Contains(t, text, "not available in this session")
	assert.Contains(t, text, "kagent/helm-agent")
}

// TestInvokeAgent_InvalidRef verifies that invoke_agent rejects refs that
// do not follow the namespace/name format.
func TestInvokeAgent_InvalidRef(t *testing.T) {
	handler := newTestHandler(t)

	sess := newMCPSession(t, handler, "/mcp")
	resp := sess.call(t, "invoke_agent", map[string]any{
		"agent": "no-slash-here",
		"task":  "do something",
	})

	text, isError := extractToolResult(t, resp)
	assert.True(t, isError)
	assert.Contains(t, text, "namespace/name")
}
