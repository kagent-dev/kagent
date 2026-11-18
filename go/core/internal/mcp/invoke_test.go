package mcp

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
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

func TestInvokeAgentTargets(t *testing.T) {
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
			h, err := New(Config{Agents: testAgentService(t), Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: &a2asrv.InterceptedHandler{Handler: gateway}})
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
			response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": "invoke_agent", "arguments": arguments}, test.tasks)
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

func TestInvokeAgentRejectsInvalidTargets(t *testing.T) {
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
				h, err := New(Config{Agents: testAgentService(t), Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: &a2asrv.InterceptedHandler{Handler: gateway}})
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

func TestToolSchemasHaveNoTopLevelCombinators(t *testing.T) {
	t.Run("Given the core MCP server, when a client lists its tools, assert that no input schema has a top-level oneOf, anyOf or allOf", func(t *testing.T) {
		// Gemini rejects these, which fails every model request from an agent that binds the tool.
		h, err := New(Config{Agents: testAgentService(t), Sessions: testSessionService(), Checkpoints: testCheckpointService(), Gateway: &fakeGateway{}})
		require.NoError(t, err)
		server := httptest.NewServer(h)
		t.Cleanup(server.Close)

		listed := rawMCPCall(t, server.URL, "tools/list", map[string]any{}, false)

		tools := listed["result"].(map[string]any)["tools"].([]any)
		require.NotEmpty(t, tools)
		names := make([]string, 0, len(tools))
		for _, value := range tools {
			tool := value.(map[string]any)
			names = append(names, tool["name"].(string))
			schema := tool["inputSchema"].(map[string]any)
			for _, combinator := range []string{"oneOf", "anyOf", "allOf"} {
				require.NotContains(t, schema, combinator, "tool %s", tool["name"])
			}
		}
		require.Contains(t, names, invokeToolName)
	})
}

func TestTaskReply(t *testing.T) {
	agentMessage := func(text string) *a2atype.Message {
		return a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(text))
	}
	artifact := func(parts ...*a2atype.Part) *a2atype.Artifact {
		return &a2atype.Artifact{ID: a2atype.NewArtifactID(), Parts: parts}
	}
	for _, test := range []struct {
		name string
		task *a2atype.Task
		want string
	}{
		{
			name: "status message",
			task: &a2atype.Task{Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Message: agentMessage("The answer is 4.")}},
			want: "The answer is 4.",
		},
		{
			name: "artifacts joined in order",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{artifact(a2atype.NewTextPart("Let me check.")), artifact(a2atype.NewTextPart("The answer is 4."))},
			},
			want: "Let me check.\nThe answer is 4.",
		},
		{
			name: "streamed text parts concatenated within an artifact",
			task: &a2atype.Task{
				Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{
					artifact(a2atype.NewTextPart("hel"), a2atype.NewTextPart("lo")),
					artifact(a2atype.NewTextPart("wor"), a2atype.NewTextPart("ld")),
				},
			},
			want: "hello\nworld",
		},
		{
			name: "structured result apart from the text around it",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{artifact(a2atype.NewTextPart("Here:"), apia2a.NewStructuredOutputPart(map[string]any{"answer": 4}, "digest"))},
			},
			want: "Here:\n{\"answer\":4}",
		},
		{
			name: "failed task keeps its error and partial output",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateFailed, Message: agentMessage("model unavailable")},
				Artifacts: []*a2atype.Artifact{artifact(a2atype.NewTextPart("Let me check."))},
			},
			want: "model unavailable\nLet me check.",
		},
		{
			name: "tool activity is not the reply",
			task: &a2atype.Task{
				Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{
					artifact(toolCallPart()),
					artifact(toolResultPart("two lines")),
					artifact(a2atype.NewTextPart("The file has two lines.")),
				},
			},
			want: "The file has two lines.",
		},
		{
			name: "structured result as JSON",
			task: &a2atype.Task{
				Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{artifact(apia2a.NewStructuredOutputPart(map[string]any{"answer": 4}, "digest"))},
			},
			want: `{"answer":4}`,
		},
		{
			name: "latest agent message of the history",
			task: &a2atype.Task{
				Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				History: []*a2atype.Message{
					a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?")),
					agentMessage("Let me think."),
					agentMessage("The answer is 4."),
				},
			},
			want: "The answer is 4.",
		},
		{
			name: "no reply yet",
			task: &a2atype.Task{Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}},
			want: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, taskReply(test.task))
			if test.want != "" {
				require.Equal(t, test.want, taskText(test.task), "invoke_agent answers the same reply")
			}
		})
	}
}

func TestTaskTextAnswersATaskWithoutReplyAsJSON(t *testing.T) {
	task := &a2atype.Task{ID: "task-0", ContextID: testSessionID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	var decoded a2atype.Task
	require.NoError(t, json.Unmarshal([]byte(taskText(task)), &decoded))
	require.Equal(t, task.ID, decoded.ID)
	require.Equal(t, a2atype.TaskStateWorking, decoded.Status.State)
}
