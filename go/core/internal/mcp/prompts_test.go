package mcp

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestSandboxPrompts(t *testing.T) {
	// An unconfigured service makes any accidental runtime I/O fail. Retrieving
	// a workflow must only render instructions, never create or inspect resources.
	handler, err := New(testSessionService(), testCheckpointService(), &a2asrv.InterceptedHandler{Handler: &fakeGateway{}}, &sandbox.Service{}, nil)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	require.NotEmpty(t, session.InitializeResult().Instructions)
	require.NotNil(t, session.InitializeResult().Capabilities.Prompts)
	listed, err := session.ListPrompts(t.Context(), nil)
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Prompts))
	for _, prompt := range listed.Prompts {
		names = append(names, prompt.Name)
	}
	require.ElementsMatch(t, []string{"sandbox-task", "sandbox-recovery"}, names)
	for _, test := range []struct {
		name      string
		prompt    string
		arguments map[string]string
		wantError bool
	}{
		{name: "task", prompt: "sandbox-task", arguments: map[string]string{"task": "Summarize input.json\nPreserve quoted text: \"sample\"", "namespace": "team-a", "template": "python"}},
		{name: "discovery", prompt: "sandbox-task", arguments: map[string]string{"task": "Run offline tests"}},
		{name: "recovery", prompt: "sandbox-recovery", arguments: map[string]string{"sandbox_id": testSessionID}},
		{name: "missing task", prompt: "sandbox-task", wantError: true},
		{name: "blank task", prompt: "sandbox-task", arguments: map[string]string{"task": "  "}, wantError: true},
		{name: "template needs namespace", prompt: "sandbox-task", arguments: map[string]string{"task": "test", "template": "python"}, wantError: true},
		{name: "unknown argument", prompt: "sandbox-task", arguments: map[string]string{"task": "test", "typo": "x"}, wantError: true},
		{name: "invalid sandbox", prompt: "sandbox-recovery", arguments: map[string]string{"sandbox_id": "invalid"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: test.prompt, Arguments: test.arguments})
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.Messages, 2)
			for _, message := range result.Messages {
				require.Equal(t, mcp.Role("user"), message.Role)
			}
			var parameters map[string]string
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(result.Messages[1].Content.(*mcp.TextContent).Text, "Task parameters:\n")), &parameters))
			require.Equal(t, test.arguments, parameters)
		})
	}
}

func TestSandboxPromptsAbsentWithoutSandboxService(t *testing.T) {
	handler, err := New(testSessionService(), testCheckpointService(), &a2asrv.InterceptedHandler{Handler: &fakeGateway{}}, nil, nil)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	require.Nil(t, session.InitializeResult().Capabilities.Prompts)
}
