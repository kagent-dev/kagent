package mcp

import (
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
)

type invocationGateway struct {
	fakeGateway
	contextID string
	messageID string
	sends     int
}

func (g *invocationGateway) SendStreamingMessage(ctx context.Context, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	g.mu.Lock()
	g.contextID, g.messageID = request.Message.ContextID, request.Message.ID
	g.sends++
	g.mu.Unlock()
	// Simulate A2A assigning the Session ID; the request must reach this
	// boundary without one when targeting an Agent.
	if request.Message.ContextID == "" {
		request.Message.ContextID = testSessionID
	}
	return g.fakeGateway.SendStreamingMessage(ctx, request)
}

func TestInvokeSessionTargets(t *testing.T) {
	for _, test := range []struct {
		name  string
		agent bool
		tasks bool
	}{
		{name: "existing synchronous"},
		{name: "new synchronous", agent: true},
		{name: "existing task", tasks: true},
		{name: "new task", agent: true, tasks: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := &invocationGateway{fakeGateway: fakeGateway{completeOnDrain: true}}
			h, err := New(testSessionService(), testCheckpointService(), &a2asrv.InterceptedHandler{Handler: gateway}, nil, nil)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.ServeHTTP(w, r.WithContext(authContext()))
			}))
			t.Cleanup(server.Close)
			arguments := map[string]any{"message": "hello", "message_id": "creation-retry"}
			if test.agent {
				arguments["agent"] = map[string]any{"namespace": "team-a", "name": "assistant"}
			} else {
				arguments["session_id"] = testSessionID
			}
			response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": invokeToolName, "arguments": arguments}, test.tasks)
			result := response["result"].(map[string]any)
			require.NotEqual(t, true, result["isError"], "%v", result)
			gateway.mu.Lock()
			contextID, messageID, sends := gateway.contextID, gateway.messageID, gateway.sends
			gateway.mu.Unlock()
			if test.agent {
				require.Empty(t, contextID, "A2A must own Session creation")
			} else {
				require.Equal(t, testSessionID, contextID)
			}
			require.Equal(t, "creation-retry", messageID)
			require.Equal(t, 1, sends)
			if test.tasks {
				require.Equal(t, "task", result["resultType"])
				handle := result["taskId"].(string)
				ref, err := decodeTaskReference(handle)
				require.NoError(t, err)
				require.Equal(t, testSessionID, ref.SessionID)
				get := rawMCPCall(t, server.URL, "tasks/get", map[string]any{"taskId": handle}, true)
				require.Equal(t, "working", get["result"].(map[string]any)["status"])
			} else {
				output := result["structuredContent"].(map[string]any)
				require.Equal(t, testSessionID, output["session_id"])
				require.Equal(t, testSessionID, output["context_id"])
				require.Equal(t, testTaskID, output["task_id"])
				require.Equal(t, "done", output["text"])
			}
		})
	}
}

func TestInvokeSessionRejectsInvalidTargets(t *testing.T) {
	for _, tasks := range []bool{false, true} {
		for _, test := range []struct {
			name      string
			arguments map[string]any
		}{
			{name: "no target", arguments: map[string]any{"message": "hello"}},
			{name: "both targets", arguments: map[string]any{"session_id": testSessionID, "agent": map[string]any{"namespace": "team-a", "name": "assistant"}, "message": "hello"}},
			{name: "blank message", arguments: map[string]any{"agent": map[string]any{"namespace": "team-a", "name": "assistant"}, "message": " "}},
			{name: "unknown session", arguments: map[string]any{"session_id": "33333333-3333-4333-8333-333333333333", "message": "hello"}},
		} {
			name := test.name
			if tasks {
				name += " with tasks"
			}
			t.Run(name, func(t *testing.T) {
				gateway := &invocationGateway{}
				h, err := New(testSessionService(), testCheckpointService(), &a2asrv.InterceptedHandler{Handler: gateway}, nil, nil)
				require.NoError(t, err)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					h.ServeHTTP(w, r.WithContext(authContext()))
				}))
				t.Cleanup(server.Close)
				response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": invokeToolName, "arguments": test.arguments}, tasks)
				if result, ok := response["result"].(map[string]any); ok {
					require.Equal(t, true, result["isError"], "%v", result)
				} else {
					require.NotNil(t, response["error"], "%v", response)
				}
				gateway.mu.Lock()
				require.Zero(t, gateway.sends, "invalid requests must not dispatch")
				gateway.mu.Unlock()
			})
		}
	}
}
