package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

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

func (s *sessionStore) UpdateSessionName(_ context.Context, id, _, name string) (*apiv1.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.byID[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	session.Name = name
	return session, nil
}

func (s *sessionStore) ListSessions(_ context.Context, query database.SessionQuery) ([]*apiv1.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*apiv1.Session
	for _, session := range s.byID {
		if query.Agent == nil || proto.Equal(session.GetAgent(), query.Agent) {
			out = append(out, session)
		}
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
	var matching []*a2atype.Task
	for _, task := range g.tasks {
		if task.ContextID == request.ContextID {
			matching = append(matching, task)
		}
	}
	start := 0
	if request.PageToken != "" {
		start, _ = strconv.Atoi(request.PageToken)
	}
	end := len(matching)
	if request.PageSize > 0 {
		end = min(start+request.PageSize, end)
	}
	response := &a2atype.ListTasksResponse{TotalSize: len(matching)}
	for _, task := range matching[start:end] {
		listed := *task
		if request.HistoryLength != nil && *request.HistoryLength == 0 {
			listed.History = nil
		}
		response.Tasks = append(response.Tasks, &listed)
	}
	if end < len(matching) {
		response.NextPageToken = strconv.Itoa(end)
	}
	return response, nil
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
	mcp.AddTool(server, &mcp.Tool{Name: invokeToolName}, h.invokeSession)
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
	require.ElementsMatch(t, []string{"name", "namespace"}, schema.Properties["agent"].Required)
	require.Equal(t, 128, *schema.Properties["request_id"].MaxLength)
	require.Equal(t, 200, *schema.Properties["name"].MaxLength)
	require.NotEmpty(t, schema.Properties["agent"].Properties["namespace"].Description)
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

// artifactTurn is a turn as the runtimes record it: the user's message in the
// history, the tool call, its result and the reply as artifacts, every entry
// with its timeline position. The history lists the reply's status message
// before the artifacts that precede it.
func artifactTurn(id string, response any) *a2atype.Task {
	at := func(carrier a2atype.MetadataCarrier, second int) {
		apia2a.SetTimelinePosition(carrier, time.Date(2026, 11, 2, 12, 0, second, 0, time.UTC))
	}
	user := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("read it"))
	at(user, 0)
	status := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("done"))
	at(status, 4)
	artifacts := []*a2atype.Artifact{
		{ID: "a-call", Parts: a2atype.ContentParts{apia2a.NewToolCallPart("call-1", "read_file", map[string]any{"path": "/etc/hosts"})}},
		{ID: "a-result", Parts: a2atype.ContentParts{apia2a.NewToolResultPart("call-1", "read_file", response)}},
		{ID: "a-text", Parts: a2atype.ContentParts{a2atype.NewTextPart("the file has two lines")}},
	}
	for i, artifact := range artifacts {
		at(artifact, i+1)
	}
	return &a2atype.Task{
		ID: a2atype.TaskID(id), ContextID: testSessionID,
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Message: status},
		History:   []*a2atype.Message{user, status},
		Artifacts: artifacts,
	}
}

func readySession() *apiv1.Session {
	return &apiv1.Session{
		Id: testSessionID, ContextId: testSessionID, State: apiv1.RuntimeState_RUNTIME_STATE_READY,
		Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}},
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

func TestGetSessionReadsToolCallsFromArtifacts(t *testing.T) {
	gateway := &turnGateway{tasks: []*a2atype.Task{artifactTurn("task-0", map[string]any{"result": "127.0.0.1 localhost"})}}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	summary := structured[GetSessionOutput](t, callTool(t, server, getSessionToolName, map[string]any{"session_id": testSessionID}))
	require.Equal(t, []ToolCallStub{{Index: 0, ID: "call-1", Name: "read_file"}}, summary.Turns[0].ToolCalls)
	require.Equal(t, "read it", summary.Turns[0].User)

	call := structured[GetSessionTurnOutput](t, callTool(t, server, getSessionTurnToolName, map[string]any{
		"session_id": testSessionID, "turn": 0, "tool_call": "call-1",
	}))
	require.NotNil(t, call.ToolCall)
	require.False(t, call.ToolCall.Pending)
	require.Equal(t, "127.0.0.1 localhost", call.ToolCall.Response)

	transcript := structured[GetSessionTurnOutput](t, callTool(t, server, getSessionTurnToolName, map[string]any{"session_id": testSessionID, "turn": 0}))
	require.Equal(t, 5, transcript.TotalMessages)
	roles := make([]string, 0, len(transcript.Messages))
	for _, message := range transcript.Messages {
		roles = append(roles, message.Role)
	}
	require.Equal(t, []string{"user", "artifact", "artifact", "artifact", "agent"}, roles, "entries follow their timeline positions")
	require.NotNil(t, transcript.Messages[1].Parts[0].ToolCall)
	require.NotNil(t, transcript.Messages[2].Parts[0].ToolResult)
	require.Equal(t, "the file has two lines", transcript.Messages[3].Parts[0].Text)
}

func TestReadTurnCountsActivityRecordedTwiceOnce(t *testing.T) {
	task := toolTurn("task-0", "read it", "done", "ok")
	task.Artifacts = []*a2atype.Artifact{
		{ID: "a-call", Parts: a2atype.ContentParts{apia2a.NewToolCallPart("call-1", "read_file", map[string]any{"path": "/etc/hosts"})}},
		{ID: "a-result", Parts: a2atype.ContentParts{apia2a.NewToolResultPart("call-1", "read_file", "ok")}},
	}
	turn := readTurn(0, task)
	require.Len(t, turn.calls, 1)
	require.True(t, turn.calls[0].answered)
	require.Equal(t, 6, turn.items(), "without timeline positions the artifacts follow the messages")
	require.Equal(t, "artifact", turn.messageView(4).Role)
	require.Equal(t, 0, turn.messageView(5).Parts[0].ToolResult.Index)
}

func TestGetSessionTurnTakesIntegerIndexes(t *testing.T) {
	gateway := &turnGateway{tasks: []*a2atype.Task{toolTurn("task-0", "read it", "done", "ok")}}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	output := structured[GetSessionTurnOutput](t, callTool(t, server, getSessionTurnToolName, map[string]any{
		"session_id": testSessionID, "turn": 0, "tool_call": 0,
	}))
	require.Equal(t, "task-0", output.TaskID)
	require.NotNil(t, output.ToolCall)
	require.Equal(t, "call-1", output.ToolCall.ID)

	require.True(t, callTool(t, server, getSessionTurnToolName, map[string]any{"session_id": testSessionID, "turn": 0.5}).IsError)
}

func TestGetSessionOfAnotherCallerIsNotFound(t *testing.T) {
	server := sessionServer(t, newSessionStore(), &turnGateway{})
	result := callTool(t, server, getSessionToolName, map[string]any{"session_id": testSessionID})
	require.True(t, result.IsError)
	require.Equal(t, "not_found", result.Meta["kagent.dev/error-code"])

	invoked := callTool(t, server, invokeToolName, map[string]any{"session_id": testSessionID, "message": "hello"})
	require.True(t, invoked.IsError)
	require.Equal(t, "not_found", invoked.Meta["kagent.dev/error-code"])
	require.Equal(t, result.Content, invoked.Content, "invoke_session words a missing Session as get_session does")
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

	other := structured[ListSessionsOutput](t, callTool(t, server, listToolName, map[string]any{
		"all_states": true, "agent": map[string]any{"namespace": "team-b", "name": "assistant"},
	}))
	require.Empty(t, other.Sessions)

	result, err := sessionToolCall(t, server, listToolName, map[string]any{"agent": map[string]any{"name": "assistant"}})
	require.True(t, err != nil || result.IsError, "accepted an agent without namespace")
}

func TestDeleteSessionTool(t *testing.T) {
	server := sessionServer(t, newSessionStore(readySession()), &turnGateway{})
	output := structured[SessionOutput](t, callTool(t, server, deleteSessionToolName, map[string]any{"session_id": testSessionID}))
	require.Equal(t, testSessionID, output.Session.ID)
}

func TestSessionToolsAnswerTheirJSONAsText(t *testing.T) {
	gateway := &turnGateway{tasks: []*a2atype.Task{toolTurn("task-0", "What is 2+2?", "The answer is 4.", "ok")}}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	for _, call := range []struct {
		tool      string
		arguments map[string]any
	}{
		{createSessionToolName, map[string]any{"agent": map[string]any{"namespace": "team-a", "name": "assistant"}, "message": "hello"}},
		{getSessionToolName, map[string]any{"session_id": testSessionID}},
		{getSessionTurnToolName, map[string]any{"session_id": testSessionID, "turn": 0}},
		{renameSessionToolName, map[string]any{"session_id": testSessionID, "name": "Arithmetic"}},
		{deleteSessionToolName, map[string]any{"session_id": testSessionID}},
	} {
		t.Run(call.tool, func(t *testing.T) {
			result := callTool(t, server, call.tool, call.arguments)
			require.False(t, result.IsError, "tool error: %v", result.Content)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok, "content is %T", result.Content[0])
			structuredJSON, err := json.Marshal(result.StructuredContent)
			require.NoError(t, err)
			require.JSONEq(t, string(structuredJSON), text.Text)
			if call.tool == getSessionToolName || call.tool == getSessionTurnToolName {
				require.Contains(t, text.Text, "The answer is 4.")
			}
		})
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
	// create_session without request_id creates another Session, and its first
	// message starts an agent turn that may run any tool.
	if create := annotations[createSessionToolName]; create != nil {
		require.False(t, create.IdempotentHint)
		require.True(t, create.DestructiveHint == nil || *create.DestructiveHint)
	}
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
		"mcp structured mirrored in text": {
			in:   map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"rows":[1,2]}`}}, "structuredContent": map[string]any{"rows": []any{float64(1), float64(2)}}},
			want: map[string]any{"rows": []any{float64(1), float64(2)}},
		},
		"mcp structured without content": {
			in:   map[string]any{"content": []any{}, "structuredContent": map[string]any{"rows": []any{}}},
			want: map[string]any{"rows": []any{}},
		},
		"mcp structured beside a summary stays whole": {
			in: map[string]any{
				"content":           []any{map[string]any{"type": "text", "text": "2 rows"}},
				"structuredContent": map[string]any{"rows": []any{float64(1), float64(2)}},
			},
			want: map[string]any{
				"content":           []any{map[string]any{"type": "text", "text": "2 rows"}},
				"structuredContent": map[string]any{"rows": []any{float64(1), float64(2)}},
			},
		},
		"mcp structured beside a summary reports its error": {
			in:     map[string]any{"content": []any{map[string]any{"type": "text", "text": "failed"}}, "structuredContent": map[string]any{"code": "x"}, "isError": true},
			want:   map[string]any{"content": []any{map[string]any{"type": "text", "text": "failed"}}, "structuredContent": map[string]any{"code": "x"}, "isError": true},
			failed: true,
		},
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

func TestGetSessionAddressesTurnsPastOnePage(t *testing.T) {
	gateway := &turnGateway{}
	for i := range 1100 {
		gateway.tasks = append(gateway.tasks, toolTurn(fmt.Sprintf("task-%d", i), fmt.Sprintf("question %d", i), "ok", "ok"))
	}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	last := structured[GetSessionOutput](t, callTool(t, server, getSessionToolName, map[string]any{"session_id": testSessionID, "last_turns": 1}))
	require.Equal(t, 1100, last.TotalTurns)
	require.Equal(t, 1099, last.Turns[0].Index)
	require.Equal(t, "question 1099", last.Turns[0].User)

	for ref, want := range map[string]string{"0": "task-0", "1099": "task-1099", "last": "task-1099", "task-120": "task-120"} {
		turn := structured[GetSessionTurnOutput](t, callTool(t, server, getSessionTurnToolName, map[string]any{"session_id": testSessionID, "turn": ref}))
		require.Equal(t, want, turn.TaskID, ref)
	}
	require.True(t, callTool(t, server, getSessionTurnToolName, map[string]any{"session_id": testSessionID, "turn": "1100"}).IsError)
}

func TestRenameSession(t *testing.T) {
	server := sessionServer(t, newSessionStore(readySession()), &turnGateway{})

	named := structured[SessionOutput](t, callTool(t, server, renameSessionToolName, map[string]any{"session_id": testSessionID, "name": "Release notes"}))
	require.Equal(t, "Release notes", named.Session.Name)

	cleared := structured[SessionOutput](t, callTool(t, server, renameSessionToolName, map[string]any{"session_id": testSessionID, "name": ""}))
	require.Empty(t, cleared.Session.Name)

	require.True(t, callTool(t, server, renameSessionToolName, map[string]any{"session_id": testSessionID, "name": " leading space"}).IsError)
}

// longTurn is a turn of n agent messages of size bytes each after its user
// message.
func longTurn(n, size int) *a2atype.Task {
	history := []*a2atype.Message{a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("go"))}
	for i := range n {
		history = append(history, a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(fmt.Sprintf("%d:%s", i, strings.Repeat("x", size)))))
	}
	return &a2atype.Task{ID: "task-0", ContextID: testSessionID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}, History: history}
}

func TestGetSessionTurnPagesByMessage(t *testing.T) {
	gateway := &turnGateway{tasks: []*a2atype.Task{longTurn(12, 1000)}}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	for _, detail := range []string{detailTranscript, detailFull} {
		t.Run(detail, func(t *testing.T) {
			var seen int
			from := 0
			for pages := 0; ; pages++ {
				require.Less(t, pages, 20, "paging does not advance")
				result := callTool(t, server, getSessionTurnToolName, map[string]any{
					"session_id": testSessionID, "turn": "0", "detail": detail, "from_message": from, "max_bytes": minSessionMaxBytes,
				})
				page := structured[GetSessionTurnOutput](t, result)
				require.LessOrEqual(t, jsonSize(result.StructuredContent), minSessionMaxBytes)
				require.Equal(t, 13, page.TotalMessages)
				seen += len(page.Messages) + len(page.History)
				if page.NextMessage == 0 {
					require.False(t, page.Truncated)
					break
				}
				require.True(t, page.Truncated)
				require.Greater(t, page.NextMessage, from)
				from = page.NextMessage
			}
			require.Equal(t, 13, seen)
		})
	}
}

func TestGetSessionTurnFullAnswersA2AMessages(t *testing.T) {
	gateway := &turnGateway{tasks: []*a2atype.Task{toolTurn("task-0", "read it", "done", "ok")}}
	server := sessionServer(t, newSessionStore(readySession()), gateway)

	full := structured[GetSessionTurnOutput](t, callTool(t, server, getSessionTurnToolName, map[string]any{"session_id": testSessionID, "turn": "0", "detail": detailFull}))
	require.Len(t, full.History, full.TotalMessages)
	require.Empty(t, full.Messages)
	data, err := json.Marshal(full.History[0])
	require.NoError(t, err)
	var message a2atype.Message
	require.NoError(t, json.Unmarshal(data, &message))
	require.Equal(t, "read it", message.Parts[0].Text())
}

func TestFitTurnStaysWithinBudget(t *testing.T) {
	in := Turn{Index: 3, TaskID: "task-3", State: "TASK_STATE_COMPLETED", User: strings.Repeat("u", 5000), Reply: strings.Repeat("r", 5000),
		ToolCalls: []ToolCallStub{{Index: 0, Name: "read_file"}}}
	out, ok := fitTurn(in, 600)
	require.True(t, ok)
	require.LessOrEqual(t, jsonSize(out), 600)
	require.True(t, out.Clipped)
	require.Nil(t, out.ToolCalls)
	require.Contains(t, out.Reply, "bytes clipped]")

	_, ok = fitTurn(in, 10)
	require.False(t, ok)
}

func TestFitMessageStaysWithinBudget(t *testing.T) {
	in := TurnMessage{Index: 1, Role: "agent", Parts: []PartView{{Text: strings.Repeat("a", 4000)}, {Text: strings.Repeat("b", 4000)}}}
	out := fitMessage(in, 1000)
	require.LessOrEqual(t, jsonSize(out), 1000)
	require.True(t, out.Clipped)
	require.Len(t, out.Parts, 2)
	require.Len(t, in.Parts[0].Text, 4000, "input is left unchanged")

	require.Nil(t, fitMessage(in, 40).Parts)
}

func TestToolCallDetailStaysWithinBudget(t *testing.T) {
	task := toolTurn("task-0", "read it", "done", map[string]any{"result": strings.Repeat("y", 20000)})
	turn := readTurn(0, task)
	call := turn.calls[0]

	whole, clipped := turn.toolCallDetail(call, 1<<20)
	require.False(t, clipped)
	require.Equal(t, strings.Repeat("y", 20000), whole.Response)

	small, clipped := turn.toolCallDetail(call, 2000)
	require.True(t, clipped)
	require.LessOrEqual(t, jsonSize(small), 2000)
	require.NotNil(t, small.Args, "small arguments stay whole")
	require.Nil(t, small.Response)
	require.Contains(t, small.ResponseText, "bytes clipped]")
}

func TestToolCallDetailPendingCallWithLargeArgs(t *testing.T) {
	call := &toolCall{id: "c", name: "write_file", args: map[string]any{"content": strings.Repeat("x", 20000)}}
	out, clipped := (&turn{}).toolCallDetail(call, 4096)
	require.True(t, clipped)
	require.True(t, out.Pending)
	require.Nil(t, out.Response, "a pending call has no response to show")
	_, err := json.Marshal(out)
	require.NoError(t, err)
	require.LessOrEqual(t, jsonSize(out), 4096)
	require.Contains(t, out.ArgsText, "bytes clipped]")
}
