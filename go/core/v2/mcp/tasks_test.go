package mcp

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/v2/a2agateway"
	"github.com/kagent-dev/kagent/go/core/v2/agentinstance"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	testInstanceID = "11111111-1111-4111-8111-111111111111"
	testTaskID     = "22222222-2222-4222-8222-222222222222"
)

func TestTaskReferenceRoundTrip(t *testing.T) {
	want := taskReference{
		Namespace: "team-a", InstanceID: testInstanceID, TaskID: testTaskID,
	}
	encoded, err := encodeTaskReference(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeTaskReference(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decodeTaskReference() = %#v, want %#v", got, want)
	}
	for _, value := range []string{"", "v2.invalid", "v1.not-base64"} {
		if _, err := decodeTaskReference(value); err == nil {
			t.Fatalf("decodeTaskReference(%q) succeeded", value)
		}
	}
}

func TestTaskStateTranslation(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		state a2atype.TaskState
		want  string
	}{
		{a2atype.TaskStateSubmitted, "working"},
		{a2atype.TaskStateWorking, "working"},
		{a2atype.TaskStateInputRequired, "input_required"},
		{a2atype.TaskStateCompleted, "completed"},
		{a2atype.TaskStateFailed, "completed"},
		{a2atype.TaskStateCanceled, "cancelled"},
	} {
		t.Run(test.state.String(), func(t *testing.T) {
			task := &a2atype.Task{ID: testTaskID, ContextID: testInstanceID, Status: a2atype.TaskStatus{State: test.state, Timestamp: &now}}
			if got := taskStatus(task); got != test.want {
				t.Fatalf("taskStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTaskTimestampsUseDurableCreationTime(t *testing.T) {
	created := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	updated := created.Add(time.Hour)
	task := &a2atype.Task{
		ID: testTaskID, ContextID: testInstanceID,
		Status:   a2atype.TaskStatus{State: a2atype.TaskStateWorking, Timestamp: &updated},
		Metadata: map[string]any{a2agateway.TaskCreatedAtMetadataKey: created.Format(time.RFC3339Nano)},
	}
	fields := taskToMCP("task-ref", task)
	if fields.CreatedAt != created.Format(time.RFC3339Nano) || fields.LastUpdatedAt != updated.Format(time.RFC3339Nano) {
		t.Fatalf("task timestamps = %q/%q", fields.CreatedAt, fields.LastUpdatedAt)
	}
}

func TestDetailedTaskIncludesInvocationOutput(t *testing.T) {
	now := time.Now().UTC()
	task := &a2atype.Task{
		ID: testTaskID, ContextID: testInstanceID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &now},
	}
	result := detailedTask("task-ref", taskReference{Namespace: "team-a", InstanceID: testInstanceID}, task)
	output, ok := result.Result.StructuredContent.(InvokeAgentInstanceOutput)
	if !ok || output.TaskID != testTaskID || output.ContextID != testInstanceID {
		t.Fatalf("structured invocation output = %#v", result.Result.StructuredContent)
	}
}

func TestTaskUpdateContinuesA2ATask(t *testing.T) {
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Which database?"))
	gateway := &fakeGateway{task: &a2atype.Task{
		ID: testTaskID, ContextID: testInstanceID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: message},
	}}
	h := &Handler{gateway: gateway}
	ref, err := encodeTaskReference(taskReference{
		Namespace: "team-a", InstanceID: testInstanceID, TaskID: testTaskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.updateTask(authContext(), nil, &updateTaskParams{
		ParamsBase: taskParamsBase(),
		TaskID:     ref,
		InputResponses: mcp.InputResponseMap{message.ID: &mcp.ElicitResult{
			Action: "accept", Content: map[string]any{"response": "PostgreSQL"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gateway.mu.Lock()
	if gateway.reply == nil || gateway.reply.TaskID != testTaskID || a2aText(gateway.reply) != "PostgreSQL" {
		t.Fatalf("continued message = %#v", gateway.reply)
	}
	gateway.mu.Unlock()
	if _, err := h.updateTask(authContext(), nil, &updateTaskParams{
		ParamsBase: taskParamsBase(),
		TaskID:     ref,
		InputResponses: mcp.InputResponseMap{message.ID: &mcp.ElicitResult{
			Action: "accept", Content: map[string]any{"response": "PostgreSQL"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.replies != 1 {
		t.Fatalf("continuation dispatches = %d, want 1", gateway.replies)
	}
}

func TestTaskUpdateTranslatesAskUserResponse(t *testing.T) {
	status := adka2a.AttachHitlExtension(
		a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Which database?")),
		&adka2a.AskUserRequest{
			Type: adka2a.HITLTypeAskUserRequest, ID: "question-1",
			Questions: []map[string]any{{"question": "Which database?", "choices": []string{"PostgreSQL", "MySQL"}}},
		},
	)
	gateway := &fakeGateway{task: &a2atype.Task{
		ID: testTaskID, ContextID: testInstanceID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: status},
	}}
	h := &Handler{gateway: gateway}
	ref, err := encodeTaskReference(taskReference{
		Namespace: "team-a", InstanceID: testInstanceID, TaskID: testTaskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.updateTask(authContext(), nil, &updateTaskParams{
		ParamsBase: taskParamsBase(), TaskID: ref,
		InputResponses: mcp.InputResponseMap{status.ID: &mcp.ElicitResult{
			Action: "accept", Content: map[string]any{"response": "PostgreSQL"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	gateway.mu.Lock()
	response := adka2a.GetAskUserResponse(gateway.reply)
	text := a2aText(gateway.reply)
	gateway.mu.Unlock()
	if response == nil || response.ID != "question-1" || len(response.Answers) != 1 || response.Answers[0].Answer[0] != "PostgreSQL" || text != "PostgreSQL" {
		t.Fatalf("ask_user response = %#v", response)
	}
}

func TestTaskCapableToolCallReturnsDurableHandle(t *testing.T) {
	gateway := &fakeGateway{}
	h, err := New(testAgentInstanceService(), &a2asrv.InterceptedHandler{Handler: gateway})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(authContext()))
	}))
	defer server.Close()

	discover := rawMCPCall(t, server.URL, "server/discover", map[string]any{}, true)
	capabilities := discover["result"].(map[string]any)["capabilities"].(map[string]any)
	extensions := capabilities["extensions"].(map[string]any)
	if extensions[tasksExtension] == nil {
		t.Fatalf("server capabilities = %#v", capabilities)
	}

	call := rawMCPCall(t, server.URL, "tools/call", map[string]any{
		"name": invokeToolName,
		"arguments": map[string]any{
			"namespace": "team-a", "agent_instance_id": testInstanceID, "message": "hello",
		},
	}, true)
	result := call["result"].(map[string]any)
	if result["resultType"] != "task" || result["status"] != "working" {
		t.Fatalf("tools/call result = %#v", result)
	}
	taskID, ok := result["taskId"].(string)
	if !ok || taskID == "" {
		t.Fatalf("taskId = %#v", result["taskId"])
	}

	get := rawMCPCall(t, server.URL, "tasks/get", map[string]any{"taskId": taskID}, true)
	got := get["result"].(map[string]any)
	if got["taskId"] != taskID || got["status"] != "working" {
		t.Fatalf("tasks/get result = %#v", got)
	}
}

func TestToolCallWithoutTasksWaitsForResult(t *testing.T) {
	gateway := &fakeGateway{completeOnDrain: true}
	h, err := New(testAgentInstanceService(), &a2asrv.InterceptedHandler{Handler: gateway})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(authContext()))
	}))
	defer server.Close()

	call := rawMCPCall(t, server.URL, "tools/call", map[string]any{
		"name": invokeToolName,
		"arguments": map[string]any{
			"namespace": "team-a", "agent_instance_id": testInstanceID, "message": "hello",
		},
	}, false)
	result := call["result"].(map[string]any)
	if result["resultType"] != "complete" || result["isError"] == true {
		t.Fatalf("tools/call result = %#v", result)
	}
	content := result["content"].([]any)[0].(map[string]any)
	if content["text"] != "done" {
		t.Fatalf("tools/call content = %#v", content)
	}
}

func TestCancelTaskUsesA2AGateway(t *testing.T) {
	gateway := &fakeGateway{task: &a2atype.Task{
		ID: testTaskID, ContextID: testInstanceID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking},
	}}
	h := &Handler{gateway: gateway}
	ref, err := encodeTaskReference(taskReference{
		Namespace: "team-a", InstanceID: testInstanceID, TaskID: testTaskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.cancelTask(authContext(), nil, &cancelTaskParams{ParamsBase: taskParamsBase(), TaskID: ref}); err != nil {
		t.Fatal(err)
	}
	if gateway.task.Status.State != a2atype.TaskStateCanceled {
		t.Fatalf("task state = %s", gateway.task.Status.State)
	}
}

func TestTaskMethodsRequireCapability(t *testing.T) {
	h := &Handler{}
	_, err := h.getTask(authContext(), nil, &getTaskParams{TaskID: "ignored"})
	rpcErr, ok := err.(*jsonrpc.Error)
	if !ok || rpcErr.Code != mcp.CodeMissingRequiredClientCapabilities {
		t.Fatalf("tasks/get error = %#v", err)
	}
}

func taskParamsBase() mcp.ParamsBase {
	return mcp.ParamsBase{Meta: mcp.Meta{
		mcp.MetaKeyClientCapabilities: map[string]any{
			"extensions": map[string]any{tasksExtension: map[string]any{}},
		},
	}}
}

func rawMCPCall(t *testing.T, endpoint, method string, params map[string]any, tasks bool) map[string]any {
	t.Helper()
	extensions := map[string]any{}
	if tasks {
		extensions[tasksExtension] = map[string]any{}
	}
	params["_meta"] = map[string]any{
		mcp.MetaKeyProtocolVersion: "2026-07-28",
		mcp.MetaKeyClientInfo:      map[string]any{"name": "test", "version": "1"},
		mcp.MetaKeyClientCapabilities: map[string]any{
			"extensions": extensions,
		},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		req.Header.Set("Mcp-Name", params["name"].(string))
	} else if strings.HasPrefix(method, "tasks/") {
		req.Header.Set("Mcp-Name", params["taskId"].(string))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(body), "event:") {
		_, data, ok := strings.Cut(string(body), "data: ")
		if !ok {
			t.Fatalf("invalid MCP event stream: %s", body)
		}
		body = []byte(strings.TrimSpace(data))
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode MCP response (status %d): %v; body = %s", resp.StatusCode, err, body)
	}
	if rpcErr := result["error"]; rpcErr != nil {
		t.Fatalf("MCP error: %#v", rpcErr)
	}
	return result
}

type testSession struct{}

func (testSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: "user@example.com"}}
}

func authContext() context.Context {
	return auth.AuthSessionTo(context.Background(), testSession{})
}

type fakeGateway struct {
	mu              sync.Mutex
	task            *a2atype.Task
	reply           *a2atype.Message
	replies         int
	completeOnDrain bool
}

var _ a2asrv.RequestHandler = (*fakeGateway)(nil)

func (g *fakeGateway) GetTask(context.Context, *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.task, nil
}

func (*fakeGateway) ListTasks(context.Context, *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return nil, a2atype.ErrUnsupportedOperation
}

func (g *fakeGateway) CancelTask(context.Context, *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.task.Status.State = a2atype.TaskStateCanceled
	return g.task, nil
}

func (*fakeGateway) SendMessage(context.Context, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	return nil, a2atype.ErrUnsupportedOperation
}

func (*fakeGateway) SubscribeToTask(context.Context, *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	return func(func(a2atype.Event, error) bool) {}
}

func (g *fakeGateway) SendStreamingMessage(_ context.Context, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	g.mu.Lock()
	defer g.mu.Unlock()
	if request.Message.TaskID != "" {
		g.reply = request.Message
		g.replies++
		g.task.Status.State = a2atype.TaskStateWorking
		return func(func(a2atype.Event, error) bool) {}
	}
	request.Message.TaskID = testTaskID
	request.Message.ContextID = testInstanceID
	g.task = &a2atype.Task{
		ID: testTaskID, ContextID: testInstanceID,
		Status:  a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		History: []*a2atype.Message{request.Message},
	}
	return func(yield func(a2atype.Event, error) bool) {
		g.mu.Lock()
		task, complete := g.task, g.completeOnDrain
		g.mu.Unlock()
		if !yield(task, nil) || !complete {
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		message := a2atype.NewMessageForTask(a2atype.MessageRoleAgent, g.task, a2atype.NewTextPart("done"))
		g.task.Status = a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Message: message}
		yield(g.task, nil)
	}
}

func (*fakeGateway) GetTaskPushConfig(context.Context, *a2atype.GetTaskPushConfigRequest) (*a2atype.PushConfig, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (*fakeGateway) ListTaskPushConfigs(context.Context, *a2atype.ListTaskPushConfigRequest) (*a2atype.ListTaskPushConfigResponse, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (*fakeGateway) CreateTaskPushConfig(context.Context, *a2atype.PushConfig) (*a2atype.PushConfig, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (*fakeGateway) DeleteTaskPushConfig(context.Context, *a2atype.DeleteTaskPushConfigRequest) error {
	return a2atype.ErrPushNotificationNotSupported
}

func (*fakeGateway) GetExtendedAgentCard(context.Context, *a2atype.GetExtendedAgentCardRequest) (*a2atype.AgentCard, error) {
	return nil, a2atype.ErrUnsupportedOperation
}

type fakeInstanceStore struct{}

func (*fakeInstanceStore) CreateAgentInstance(context.Context, *apiv1alpha1.AgentInstance, string) (*apiv1alpha1.AgentInstance, bool, error) {
	return nil, false, dbpkg.ErrNotFound
}

func (*fakeInstanceStore) GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error) {
	return nil, dbpkg.ErrNotFound
}

func (*fakeInstanceStore) ListAgentInstances(context.Context, dbpkg.AgentInstanceQuery) ([]*apiv1alpha1.AgentInstance, error) {
	return []*apiv1alpha1.AgentInstance{{
		Id: testInstanceID, Namespace: "team-a", State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
		AgentTemplate: &apiv1alpha1.ResourceReference{Name: "assistant"},
		Harness:       &apiv1alpha1.ResourceReference{Name: "kagent"},
	}}, nil
}

func (*fakeInstanceStore) UpdateAgentInstanceName(context.Context, string, string, string, string) (*apiv1alpha1.AgentInstance, error) {
	return nil, dbpkg.ErrNotFound
}

func (*fakeInstanceStore) CreateAgentInstanceShare(context.Context, dbpkg.AgentInstanceShare) (*dbpkg.AgentInstanceShare, error) {
	return nil, dbpkg.ErrNotFound
}

func (*fakeInstanceStore) ListAgentInstanceShares(context.Context, string, string, string, string, int) ([]dbpkg.AgentInstanceShare, error) {
	return nil, nil
}

func (*fakeInstanceStore) DeleteAgentInstanceShare(context.Context, string, string, string) error {
	return nil
}

type fakeInstanceWorkflow struct{}

func (*fakeInstanceWorkflow) Create(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, nil
}

func (*fakeInstanceWorkflow) Suspend(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, nil
}

func (*fakeInstanceWorkflow) Resume(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, nil
}

func (*fakeInstanceWorkflow) Delete(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, nil
}

func testAgentInstanceService() *agentinstance.Service {
	return agentinstance.NewService(&fakeInstanceStore{}, &authimpl.NoopAuthorizer{}, &fakeInstanceWorkflow{})
}
