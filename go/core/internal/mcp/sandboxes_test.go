package mcp

import (
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/stretchr/testify/require"
)

func TestSandboxToolsRegisteredAndValidateBeforeDispatch(t *testing.T) {
	// A service without dependencies proves invalid input never reaches I/O.
	handler, err := New(testSessionService(), testCheckpointService(),
		&a2asrv.InterceptedHandler{Handler: &fakeGateway{}}, &sandbox.Service{}, nil)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	list := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)
	registered := map[string]bool{}
	for _, value := range list["result"].(map[string]any)["tools"].([]any) {
		tool := value.(map[string]any)
		registered[tool["name"].(string)] = true
	}
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"create_sandbox", map[string]any{"namespace": "team-a", "template": "scratch", "request_id": ""}},
		{"list_sandboxes", map[string]any{"page_size": -1}},
		{"get_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"suspend_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"resume_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"delete_sandbox", map[string]any{"sandbox_id": "invalid"}},
		{"start_sandbox_process", map[string]any{"sandbox_id": "invalid", "command": []string{}}},
		{"get_sandbox_process", map[string]any{"sandbox_id": "invalid", "process_id": "invalid"}},
		{"kill_sandbox_process", map[string]any{"sandbox_id": "invalid", "process_id": "invalid"}},
		{"read_sandbox_outputs", map[string]any{"sandbox_id": "invalid", "process_id": "invalid"}},
		{"read_sandbox_file", map[string]any{"sandbox_id": "invalid", "path": "/data/workspace/file"}},
		{"write_sandbox_file", map[string]any{"sandbox_id": "invalid", "path": "/data/workspace/file", "data_base64": ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.True(t, registered[test.name])
			result := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": test.name, "arguments": test.args}, false)
			require.Nil(t, result["error"], "tool failures must not become protocol errors")
			require.Equal(t, true, result["result"].(map[string]any)["isError"])
		})
	}
}
