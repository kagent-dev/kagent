package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCollaborationContractsThroughSDKClient(t *testing.T) {
	gateway := &fakeGateway{completeOnDrain: true, task: &a2atype.Task{
		ID: testTaskID, ContextID: testSessionID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
	}}
	handler, err := New(testSessionService(), testCheckpointService(), gateway, nil, nil)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(auth.AuthSessionTo(r.Context(), testSession{})))
	}))
	t.Cleanup(server.Close)
	client, err := mcp.NewClient(&mcp.Implementation{Name: "contracts", Version: "1"}, nil).Connect(
		t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	listed, err := client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	found := false
	for _, tool := range listed.Tools {
		if tool.Name == invokeToolName {
			found = true
			require.NotNil(t, tool.InputSchema)
			require.NotNil(t, tool.OutputSchema)
		}
	}
	require.True(t, found, "invoke_session is discoverable")
	for _, test := range []struct {
		name      string
		arguments map[string]any
		wantError bool
	}{
		{"optional message id", map[string]any{"session_id": testSessionID, "message": "hello"}, false},
		{"stable message id", map[string]any{"session_id": testSessionID, "message": "hello", "message_id": "retry-key"}, false},
		{"missing message", map[string]any{"session_id": testSessionID}, true},
		{"empty message", map[string]any{"session_id": testSessionID, "message": ""}, true},
		{"missing session", map[string]any{"message": "hello"}, true},
		{"foreign session", map[string]any{"session_id": "missing", "message": "hello"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: invokeToolName, Arguments: test.arguments})
			require.NoError(t, err)
			require.Equal(t, test.wantError, result.IsError)
			if test.wantError {
				return
			}
			encoded, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			var output map[string]any
			require.NoError(t, json.Unmarshal(encoded, &output))
			require.Equal(t, testSessionID, output["session_id"])
			require.Equal(t, testTaskID, output["task_id"])
			require.Equal(t, "TASK_STATE_COMPLETED", output["state"])
			if test.name == "stable message id" {
				require.Equal(t, "retry-key", gateway.task.History[0].ID)
			}
		})
	}
	gateway.stateOnDrain = a2atype.TaskStateFailed
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{
		Name: invokeToolName, Arguments: map[string]any{"session_id": testSessionID, "message": "fail"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var failed map[string]any
	require.NoError(t, json.Unmarshal(encoded, &failed))
	require.Equal(t, testTaskID, failed["task_id"])
	require.Equal(t, "TASK_STATE_FAILED", failed["state"])
}
