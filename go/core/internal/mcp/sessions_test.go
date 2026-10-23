package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"sync"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// sessionStore keeps the Sessions it creates, keyed by request ID as the
// database does.
type sessionStore struct {
	fakeSessionStore
	mu        sync.Mutex
	byID      map[string]*apiv1.Session
	byRequest map[string]*apiv1.Session
}

func newSessionStore(sessions ...*apiv1.Session) *sessionStore {
	store := &sessionStore{byID: map[string]*apiv1.Session{}, byRequest: map[string]*apiv1.Session{}}
	for _, session := range sessions {
		store.byID[session.Id] = session
	}
	return store
}

func (s *sessionStore) CreateSession(_ context.Context, session *apiv1.Session, requestID string) (*apiv1.Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byRequest[requestID]; ok {
		return existing, false, nil
	}
	session.State = apiv1.RuntimeState_RUNTIME_STATE_READY
	s.byID[session.Id], s.byRequest[requestID] = session, session
	return session, true, nil
}

func (s *sessionStore) GetSession(_ context.Context, id, _ string) (*apiv1.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.byID[id]; ok {
		return session, nil
	}
	return nil, database.ErrNotFound
}

func (s *sessionStore) GetSessionByID(ctx context.Context, id string) (*apiv1.Session, error) {
	return s.GetSession(ctx, id, "")
}

func (s *sessionStore) ListSessions(context.Context, database.SessionQuery) ([]*apiv1.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*apiv1.Session
	for _, session := range s.byID {
		out = append(out, session)
	}
	return out, nil
}

// turnGateway serves a Session's turns from tasks and records every message
// sent to it as a new turn.
type turnGateway struct {
	fakeGateway
	mu    sync.Mutex
	tasks []*a2atype.Task
	sent  []*a2atype.Message
}

func (g *turnGateway) ListTasks(_ context.Context, request *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []*a2atype.Task
	for _, task := range g.tasks {
		if task.ContextID != request.ContextID {
			continue
		}
		listed := *task
		if request.HistoryLength != nil && *request.HistoryLength == 0 {
			listed.History = nil
		}
		out = append(out, &listed)
		if request.PageSize > 0 && len(out) == request.PageSize {
			break
		}
	}
	return &a2atype.ListTasksResponse{Tasks: out, TotalSize: len(out)}, nil
}

func (g *turnGateway) GetTask(_ context.Context, request *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, task := range g.tasks {
		if task.ID == request.ID {
			return task, nil
		}
	}
	return nil, a2atype.ErrTaskNotFound
}

func (g *turnGateway) SendStreamingMessage(_ context.Context, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sent = append(g.sent, request.Message)
	task := &a2atype.Task{
		ID: a2atype.TaskID(fmt.Sprintf("task-%d", len(g.tasks))), ContextID: request.Message.ContextID,
		Status:  a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		History: []*a2atype.Message{request.Message},
	}
	g.tasks = append(g.tasks, task)
	return func(yield func(a2atype.Event, error) bool) { yield(task, nil) }
}

func callTool(t *testing.T, server *mcp.Server, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := sessionToolCall(t, server, name, arguments)
	require.NoError(t, err)
	return result
}

// sessionServer is the MCP server with the session tools, every call made as
// the test user.
func sessionServer(t *testing.T, store *sessionStore, gateway *turnGateway) *mcp.Server {
	t.Helper()
	h := &Handler{sessions: sessionsvc.NewService(store, &auth.NoopAuthorizer{}, &fakeSessionWorkflow{}), checkpoints: testCheckpointService(), gateway: gateway}
	server := mcp.NewServer(&mcp.Implementation{Name: "kagent", Version: "test"}, nil)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			return next(auth.AuthSessionTo(ctx, testSession{}), method, request)
		}
	})
	h.registerSessionTools(server)
	mcp.AddTool(server, &mcp.Tool{Name: listToolName}, h.listSessions)
	return server
}

func structured[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	require.False(t, result.IsError, "tool error: %v", result.Content)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func TestCreateSessionSchemaFollowsRequest(t *testing.T) {
	schema := createSessionSchema()
	require.Equal(t, []string{"agent"}, schema.Required)
	for _, name := range []string{"agent", "request_id", "name", "message"} {
		require.Contains(t, schema.Properties, name)
	}
	require.Equal(t, []string{"name", "namespace"}, sortedRequired(schema.Properties["agent"].Required))
	require.Equal(t, 128, *schema.Properties["request_id"].MaxLength)
	require.Equal(t, 200, *schema.Properties["name"].MaxLength)
	require.NotEmpty(t, schema.Properties["agent"].Properties["namespace"].Description)
}

func sortedRequired(names []string) []string {
	out := append([]string(nil), names...)
	if len(out) == 2 && out[0] > out[1] {
		out[0], out[1] = out[1], out[0]
	}
	return out
}

func TestCreateSessionSendsFirstMessageOncePerRequestID(t *testing.T) {
	store, gateway := newSessionStore(), &turnGateway{}
	server := sessionServer(t, store, gateway)
	arguments := map[string]any{
		"agent":      map[string]any{"namespace": "team-a", "name": "assistant"},
		"request_id": "retry-me",
		"name":       "Release notes",
		"message":    "Summarize the release",
	}

	first := structured[CreateSessionOutput](t, callTool(t, server, createSessionToolName, arguments))
	require.Equal(t, "team-a/assistant", first.Session.Agent)
	require.Equal(t, "Release notes", first.Session.Name)
	require.NotNil(t, first.Turn)
	require.Equal(t, a2atype.TaskStateWorking.String(), first.Turn.State)

	retry := structured[CreateSessionOutput](t, callTool(t, server, createSessionToolName, arguments))
	require.Equal(t, first.Session.ID, retry.Session.ID)
	require.Equal(t, first.Turn.TaskID, retry.Turn.TaskID)
	require.Len(t, gateway.sent, 1)
	require.Equal(t, first.Session.ID, gateway.sent[0].ContextID)
}

func TestCreateSessionWithoutMessageStartsNoTurn(t *testing.T) {
	gateway := &turnGateway{}
	server := sessionServer(t, newSessionStore(), gateway)
	output := structured[CreateSessionOutput](t, callTool(t, server, createSessionToolName, map[string]any{
		"agent": map[string]any{"namespace": "team-a", "name": "assistant"},
	}))
	require.Nil(t, output.Turn)
	require.Empty(t, gateway.sent)
}

func TestCreateSessionRefusesInvalidArguments(t *testing.T) {
	server := sessionServer(t, newSessionStore(), &turnGateway{})
	for name, arguments := range map[string]map[string]any{
		"unknown field": {"agent": map[string]any{"namespace": "team-a", "name": "assistant"}, "not_a_field": true},
		"no agent":      {"name": "orphan"},
		"blank message": {"agent": map[string]any{"namespace": "team-a", "name": "assistant"}, "message": "  "},
		"long name":     {"agent": map[string]any{"namespace": "team-a", "name": "assistant"}, "name": strings.Repeat("n", 201)},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sessionToolCall(t, server, createSessionToolName, arguments)
			require.True(t, err != nil || result.IsError, "accepted %v", arguments)
		})
	}
}

// sessionToolCall is callTool for calls the SDK itself may refuse against the
// input schema.
func sessionToolCall(t *testing.T, server *mcp.Server, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
}

func toolTurn(id string, user, reply string, response any) *a2atype.Task {
	userMessage := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(user))
	call := a2atype.NewMessage(a2atype.MessageRoleAgent, apia2a.NewToolCallPart("call-1", "read_file", map[string]any{"path": "/etc/hosts"}))
	result := a2atype.NewMessage(a2atype.MessageRoleAgent, apia2a.NewToolResultPart("call-1", "read_file", response))
	return &a2atype.Task{
		ID: a2atype.TaskID(id), ContextID: testSessionID,
		Status: a2atype.TaskStatus{
			State:   a2atype.TaskStateCompleted,
			Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(reply)),
		},
		History: []*a2atype.Message{userMessage, call, result},
	}
}

func readySession() *apiv1.Session {
	return &apiv1.Session{
		Id: testSessionID, ContextId: testSessionID, State: apiv1.RuntimeState_RUNTIME_STATE_READY,
		Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}
}

func TestGetSessionReadsTurnsWithinBudget(t *testing.T) {
	gateway := &turnGateway{}
	for i := range 3 {
		gateway.tasks = append(gateway.tasks, toolTurn(fmt.Sprintf("task-%d", i), fmt.Sprintf("question %d", i), strings.Repeat("r", 3000), "ok"))
	}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	all := structured[GetSessionOutput](t, callTool(t, server, getSessionToolName, map[string]any{"session_id": testSessionID}))
	require.Equal(t, 3, all.TotalTurns)
	require.Len(t, all.Turns, 3)
	require.Equal(t, "question 0", all.Turns[0].User)
	require.Equal(t, []ToolCallStub{{Index: 0, ID: "call-1", Name: "read_file"}}, all.Turns[0].ToolCalls)
	require.False(t, all.Truncated)

	last := structured[GetSessionOutput](t, callTool(t, server, getSessionToolName, map[string]any{"session_id": testSessionID, "last_turns": 1}))
	require.Len(t, last.Turns, 1)
	require.Equal(t, 2, last.Turns[0].Index)
	require.Equal(t, 3, last.TotalTurns)
	require.Zero(t, last.OmittedTurns)

	tight := structured[GetSessionOutput](t, callTool(t, server, getSessionToolName, map[string]any{"session_id": testSessionID, "max_bytes": minSessionMaxBytes}))
	require.Equal(t, 2, tight.Turns[len(tight.Turns)-1].Index)
	require.Positive(t, tight.OmittedTurns)
	require.True(t, tight.Truncated)
	require.NotEmpty(t, tight.Hint)
}

func TestGetSessionTurnUnwrapsToolCall(t *testing.T) {
	response := map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"lines":2}`}}, "isError": true}
	gateway := &turnGateway{tasks: []*a2atype.Task{toolTurn("task-0", "read it", "failed", response)}}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	output := structured[GetSessionTurnOutput](t, callTool(t, server, getSessionTurnToolName, map[string]any{
		"session_id": testSessionID, "turn": "last", "tool_call": "call-1",
	}))
	require.Equal(t, "task-0", output.TaskID)
	require.NotNil(t, output.ToolCall)
	require.True(t, output.ToolCall.Error)
	require.JSONEq(t, `{"path":"/etc/hosts"}`, jsonText(output.ToolCall.Args))
	require.JSONEq(t, `{"lines":2}`, jsonText(output.ToolCall.Response))

	transcript := structured[GetSessionTurnOutput](t, callTool(t, server, getSessionTurnToolName, map[string]any{
		"session_id": testSessionID, "turn": "0",
	}))
	require.Equal(t, 4, transcript.TotalMessages)
	require.NotNil(t, transcript.Messages[1].Parts[0].ToolCall)
	require.NotNil(t, transcript.Messages[2].Parts[0].ToolResult)
	require.True(t, transcript.Messages[2].Parts[0].ToolResult.Error)
}

func TestGetSessionOfAnotherCallerIsNotFound(t *testing.T) {
	server := sessionServer(t, newSessionStore(), &turnGateway{})
	result := callTool(t, server, getSessionToolName, map[string]any{"session_id": testSessionID})
	require.True(t, result.IsError)
	require.Equal(t, "not_found", result.Meta["kagent.dev/error-code"])
}

func TestListSessionsAllStatesShowsFailures(t *testing.T) {
	failed := readySession()
	failed.Id = "33333333-3333-4333-8333-333333333333"
	failed.State = apiv1.RuntimeState_RUNTIME_STATE_FAILED
	failed.Failure = &apiv1.Failure{Reason: "ImagePullBackOff", Message: "image not found"}
	server := sessionServer(t, newSessionStore(readySession(), failed), &turnGateway{})

	ready := structured[ListSessionsOutput](t, callTool(t, server, listToolName, map[string]any{}))
	require.Len(t, ready.Sessions, 1)

	all := structured[ListSessionsOutput](t, callTool(t, server, listToolName, map[string]any{"all_states": true}))
	require.Len(t, all.Sessions, 2)
	var found *SessionSummary
	for i := range all.Sessions {
		if all.Sessions[i].ID == failed.Id {
			found = &all.Sessions[i]
		}
	}
	require.NotNil(t, found)
	require.Equal(t, "ImagePullBackOff", found.Failure.Reason)

	result := callTool(t, server, listToolName, map[string]any{"agent_name": "assistant"})
	require.True(t, result.IsError)
}

func TestSessionLifecycleTools(t *testing.T) {
	server := sessionServer(t, newSessionStore(readySession()), &turnGateway{})
	for _, name := range []string{suspendSessionToolName, resumeSessionToolName, deleteSessionToolName} {
		output := structured[SessionOutput](t, callTool(t, server, name, map[string]any{"session_id": testSessionID}))
		require.Equal(t, testSessionID, output.Session.ID, name)
	}
}

func TestSessionToolAnnotations(t *testing.T) {
	server := sessionServer(t, newSessionStore(), &turnGateway{})
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	annotations := map[string]*mcp.ToolAnnotations{}
	for _, tool := range listed.Tools {
		annotations[tool.Name] = tool.Annotations
	}
	require.True(t, annotations[getSessionToolName].ReadOnlyHint)
	require.True(t, annotations[getSessionTurnToolName].ReadOnlyHint)
	require.True(t, *annotations[deleteSessionToolName].DestructiveHint)
	require.False(t, *annotations[createSessionToolName].DestructiveHint)
}

func TestUnwrapResponse(t *testing.T) {
	for name, test := range map[string]struct {
		in     any
		want   any
		failed bool
	}{
		"plain":           {in: "hello", want: "hello"},
		"runtime result":  {in: map[string]any{"result": "ok", "isError": false}, want: "ok"},
		"mcp text":        {in: map[string]any{"content": []any{map[string]any{"type": "text", "text": "out"}}}, want: "out"},
		"nested json":     {in: `{"result":"{\"a\":1}"}`, want: map[string]any{"a": float64(1)}},
		"error field":     {in: map[string]any{"error": "boom"}, want: map[string]any{"error": "boom"}, failed: true},
		"empty error":     {in: map[string]any{"error": ""}, want: map[string]any{"error": ""}},
		"mcp error layer": {in: map[string]any{"content": []any{map[string]any{"type": "text", "text": "denied"}}, "isError": true}, want: "denied", failed: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, failed := unwrapResponse(test.in)
			require.Equal(t, test.want, got)
			require.Equal(t, test.failed, failed)
		})
	}
}

func TestClipText(t *testing.T) {
	require.Equal(t, "short", clipText("short", 10))
	clipped := clipText(strings.Repeat("é", 100), 40)
	require.LessOrEqual(t, len(clipped), 40)
	require.Contains(t, clipped, "bytes clipped]")
}
