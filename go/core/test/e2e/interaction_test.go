package e2e_test

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/kagent-dev/mockmcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

//go:embed mocks/invoke_golang_adk_agent.json mocks/invoke_mcp_agent.json mocks/invoke_shared_agent.json
var interactionMocks embed.FS

// TestAgentInstanceInteraction verifies the complete public interaction path:
// gateway routing, Substrate Actor transport, Go ADK execution, and the model call.
func TestAgentInstanceInteraction(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startInteractionMock(t))
	_, _, task := fixture.send(t, "What is 2+2?")
	if task.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("A2A task state = %s, want COMPLETED", task.Status.State)
	}
	if text := taskText(task); !strings.Contains(text, "The answer is 4.") {
		t.Fatalf("A2A response text = %q, want mock LLM response", text)
	}
}

func TestAgentInstanceCheckpoint(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startInteractionMock(t))
	_, _, task := fixture.send(t, "What is 2+2?")
	created, err := fixture.checkpoints.CreateCheckpoint(fixture.ctx, &apiv1alpha1.CreateCheckpointRequest{
		Namespace: "kagent", AgentInstanceId: fixture.instanceID, RequestId: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	checkpoint := created.GetCheckpoint()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cleanupCancel()
		_, cleanupErr := fixture.checkpoints.DeleteCheckpoint(cleanupCtx, &apiv1alpha1.DeleteCheckpointRequest{
			Namespace: "kagent", CheckpointId: checkpoint.GetId(),
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete checkpoint: %v", cleanupErr)
		}
	})
	if checkpoint.GetState() != apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY ||
		checkpoint.GetHeadTaskId() != string(task.ID) || checkpoint.GetHistorySequence() == 0 {
		t.Fatalf("checkpoint = %+v, want ready boundary for task %s", checkpoint, task.ID)
	}

	got, err := fixture.checkpoints.GetCheckpoint(fixture.ctx, &apiv1alpha1.GetCheckpointRequest{
		Namespace: "kagent", CheckpointId: checkpoint.GetId(),
	})
	if err != nil || got.GetCheckpoint().GetId() != checkpoint.GetId() {
		t.Fatalf("get checkpoint = %+v, error %v", got.GetCheckpoint(), err)
	}
	listed, err := fixture.checkpoints.ListCheckpoints(fixture.ctx, &apiv1alpha1.ListCheckpointsRequest{
		Namespace: "kagent", AgentInstanceId: fixture.instanceID,
	})
	if err != nil || len(listed.GetCheckpoints()) != 1 || listed.GetCheckpoints()[0].GetId() != checkpoint.GetId() {
		t.Fatalf("list checkpoints = %+v, error %v", listed.GetCheckpoints(), err)
	}
	if _, err := fixture.checkpoints.DeleteCheckpoint(fixture.ctx, &apiv1alpha1.DeleteCheckpointRequest{
		Namespace: "kagent", CheckpointId: checkpoint.GetId(),
	}); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	if _, err := fixture.checkpoints.GetCheckpoint(fixture.ctx, &apiv1alpha1.GetCheckpointRequest{
		Namespace: "kagent", CheckpointId: checkpoint.GetId(),
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("get deleted checkpoint error = %v, want %s", err, codes.NotFound)
	}
}

func TestMCPInteraction(t *testing.T) {
	target := interactionTarget(t)
	mcpURL, mcpServer := startMCPMock(t)
	template := createMCPInteractionTemplate(t, startMockLLM(t, "mocks/invoke_mcp_agent.json"), mcpURL)
	fixture := newInteractionFixtureForTemplate(t, target, template)
	_, _, task := fixture.send(t, "add 3 and 5")
	if task.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(task), "result is 8") {
		t.Fatalf("A2A task state = %s, text = %q, want completed task with MCP result", task.Status.State, taskText(task))
	}
	for _, request := range mcpServer.Requests() {
		if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
			return
		}
	}
	t.Fatal("mock MCP server did not receive an add_numbers tool call")
}

func TestSharedAgentInteraction(t *testing.T) {
	fixture := newSharedInteractionFixture(t, interactionTarget(t))
	_, _, task := fixture.send(t, "Ask the specialist")
	if task.Status.State != a2atype.TaskStateCompleted || !strings.Contains(taskText(task), "Answer from the shared specialist.") {
		t.Fatalf("A2A task state = %s, text = %q, want completed task with shared child response", task.Status.State, taskText(task))
	}
	instances, err := fixture.instances.ListAgentInstances(fixture.ctx, &apiv1alpha1.ListAgentInstancesRequest{Namespace: "kagent"})
	if err != nil {
		t.Fatalf("list AgentInstances: %v", err)
	}
	for _, instance := range instances.GetAgentInstances() {
		if instance.GetAgentTemplate().GetName() == fixture.childTemplate {
			t.Fatalf("Shared child created AgentInstance %q", instance.GetId())
		}
	}

	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: fixture.instanceID})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	listed, err := fixture.client.ListTasks(fixture.ctx, listRequest)
	if err != nil {
		t.Fatalf("list root tasks: %v", err)
	}
	if len(listed.GetTasks()) != 1 || listed.GetTasks()[0].GetId() != string(task.ID) {
		t.Fatalf("public tasks = %#v, want only root task %s", listed.GetTasks(), task.ID)
	}
}

func TestAgentInstanceTaskPersistenceAndIdempotency(t *testing.T) {
	fixture := newInteractionFixture(t, interactionTarget(t), startInteractionMock(t))
	message, request, task := fixture.send(t, "What is 2+2?")

	getRequest, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	gotProto, err := fixture.client.GetTask(fixture.ctx, getRequest)
	if err != nil {
		t.Fatalf("get persisted task: %v", err)
	}
	got, err := pbconv.FromProtoTask(gotProto)
	if err != nil {
		t.Fatalf("decode persisted task: %v", err)
	}
	if got.ID != task.ID || got.ContextID != fixture.instanceID || got.Status.State != a2atype.TaskStateCompleted {
		t.Fatalf("persisted task = %#v, want completed task %s in context %s", got, task.ID, fixture.instanceID)
	}

	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: fixture.instanceID})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	listedProto, err := fixture.client.ListTasks(fixture.ctx, listRequest)
	if err != nil {
		t.Fatalf("list persisted tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(listedProto)
	if err != nil {
		t.Fatalf("decode listed tasks: %v", err)
	}
	if listed.TotalSize != 1 || len(listed.Tasks) != 1 || listed.Tasks[0].ID != task.ID || listed.Tasks[0].ContextID != fixture.instanceID {
		t.Fatalf("listed tasks = %#v, want only task %s in context %s", listed, task.ID, fixture.instanceID)
	}

	replayedProto, err := fixture.client.SendMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("replay A2A message: %v", err)
	}
	replayed, err := pbconv.FromProtoSendMessageResponse(replayedProto)
	if err != nil {
		t.Fatalf("decode replayed response: %v", err)
	}
	replayedTask, ok := replayed.(*a2atype.Task)
	if !ok || replayedTask.ID != task.ID {
		t.Fatalf("replayed response = %#v, want task %s", replayed, task.ID)
	}

	conflictingMessage := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 3+3?"))
	conflictingMessage.ID = message.ID
	conflictingRequest, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: conflictingMessage})
	if err != nil {
		t.Fatalf("build conflicting A2A request: %v", err)
	}
	if _, err := fixture.client.SendMessage(fixture.ctx, conflictingRequest); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("conflicting message error = %v, want %s", err, codes.InvalidArgument)
	}
}

func TestAgentInstanceActiveTask(t *testing.T) {
	target := interactionTarget(t)
	modelURL, started := startBlockingInteractionMock(t)
	fixture := newInteractionFixture(t, target, modelURL)
	_, request := newMessageRequest(t, "Wait for cancellation")
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		t.Fatalf("start streaming A2A message: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Minute):
		t.Fatal("runtime did not call the blocking model")
	}

	listRequest, err := pbconv.ToProtoListTasksRequest(&a2atype.ListTasksRequest{ContextID: fixture.instanceID})
	if err != nil {
		t.Fatalf("build ListTasks request: %v", err)
	}
	listedProto, err := fixture.client.ListTasks(fixture.ctx, listRequest)
	if err != nil {
		t.Fatalf("list active tasks: %v", err)
	}
	listed, err := pbconv.FromProtoListTasksResponse(listedProto)
	if err != nil {
		t.Fatalf("decode active tasks: %v", err)
	}
	if len(listed.Tasks) != 1 || listed.Tasks[0].Status.State.Terminal() {
		t.Fatalf("active tasks = %#v, want one non-terminal task", listed.Tasks)
	}
	task := listed.Tasks[0]

	_, busyRequest := newMessageRequest(t, "Second concurrent request")
	if _, err := fixture.client.SendMessage(fixture.ctx, busyRequest); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("concurrent message error = %v, want %s", err, codes.FailedPrecondition)
	}

	subscribeRequest, err := pbconv.ToProtoSubscribeToTaskRequest(&a2atype.SubscribeToTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatalf("build SubscribeToTask request: %v", err)
	}
	subscription, err := fixture.client.SubscribeToTask(fixture.ctx, subscribeRequest)
	if err != nil {
		t.Fatalf("subscribe to active task: %v", err)
	}
	firstEventProto, err := subscription.Recv()
	if err != nil {
		t.Fatalf("receive initial subscribed task event: %v", err)
	}
	firstEvent, err := pbconv.FromProtoStreamResponse(firstEventProto)
	if err != nil {
		t.Fatalf("decode initial subscribed task event: %v", err)
	}
	if firstEvent.TaskInfo().TaskID != task.ID {
		t.Fatalf("subscribed task = %s, want %s", firstEvent.TaskInfo().TaskID, task.ID)
	}

	cancelRequest, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatalf("build CancelTask request: %v", err)
	}
	canceledProto, err := fixture.client.CancelTask(fixture.ctx, cancelRequest)
	if err != nil {
		t.Fatalf("cancel active task: %v", err)
	}
	canceled, err := pbconv.FromProtoTask(canceledProto)
	if err != nil {
		t.Fatalf("decode canceled task: %v", err)
	}
	if canceled.ID != task.ID || canceled.Status.State != a2atype.TaskStateCanceled {
		t.Fatalf("canceled task = %#v, want task %s in CANCELED", canceled, task.ID)
	}
	waitForTaskState(t, subscription, a2atype.TaskStateCanceled)
	waitForTaskState(t, stream, a2atype.TaskStateCanceled)
	getRequest, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: task.ID})
	if err != nil {
		t.Fatalf("build GetTask request: %v", err)
	}
	persistedProto, err := fixture.client.GetTask(fixture.ctx, getRequest)
	if err != nil {
		t.Fatalf("get canceled task: %v", err)
	}
	persisted, err := pbconv.FromProtoTask(persistedProto)
	if err != nil {
		t.Fatalf("decode canceled task: %v", err)
	}
	if persisted.Status.State != a2atype.TaskStateCanceled {
		t.Fatalf("persisted task state = %s, want CANCELED", persisted.Status.State)
	}
}

type interactionFixture struct {
	ctx         context.Context
	client      a2apb.A2AServiceClient
	instances   apiv1alpha1.AgentInstanceServiceClient
	checkpoints apiv1alpha1.CheckpointServiceClient
	instanceID  string
}

type sharedInteractionFixture struct {
	*interactionFixture
	childTemplate string
}

func interactionTarget(t *testing.T) string {
	t.Helper()
	target := os.Getenv("KAGENT_E2E_GRPC_TARGET")
	if target == "" {
		target = os.Getenv("KAGENT_GRPC_URL")
	}
	if target == "" {
		t.Skip("KAGENT_E2E_GRPC_TARGET is not set")
	}
	return target
}

func newInteractionFixture(t *testing.T, target, modelURL string) *interactionFixture {
	t.Helper()
	return newInteractionFixtureForTemplate(t, target, createInteractionTemplate(t, modelURL))
}

func newInteractionFixtureForTemplate(t *testing.T, target, templateName string) *interactionFixture {
	t.Helper()
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("connect to kagent gRPC API: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 4*time.Minute)
	t.Cleanup(cancel)
	instances := apiv1alpha1.NewAgentInstanceServiceClient(conn)
	request := &apiv1alpha1.CreateAgentInstanceRequest{
		Namespace: "kagent", AgentTemplate: templateName, Harness: "kagent", RequestId: uuid.NewString(),
	}
	var created *apiv1alpha1.CreateAgentInstanceResponse
	err = wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		created, err = instances.CreateAgentInstance(ctx, request)
		if status.Code(err) == codes.FailedPrecondition {
			return false, nil
		}
		return err == nil, err
	})
	if err != nil {
		t.Fatalf("create AgentInstance: %v", err)
	}
	instance := created.GetAgentInstance()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cleanupCancel()
		_, cleanupErr := instances.DeleteAgentInstance(cleanupCtx, &apiv1alpha1.DeleteAgentInstanceRequest{
			Namespace: "kagent", AgentInstanceId: instance.GetId(),
		})
		if cleanupErr != nil && status.Code(cleanupErr) != codes.NotFound {
			t.Errorf("delete AgentInstance: %v", cleanupErr)
		}
	})
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		t.Fatalf("created AgentInstance state = %s, want READY", instance.GetState())
	}
	return &interactionFixture{
		ctx: metadata.AppendToOutgoingContext(ctx,
			"x-kagent-agent-instance-namespace", "kagent",
			"x-kagent-agent-instance-id", instance.GetId(),
		),
		client:      a2apb.NewA2AServiceClient(conn),
		instances:   instances,
		checkpoints: apiv1alpha1.NewCheckpointServiceClient(conn),
		instanceID:  instance.GetId(),
	}
}

func newSharedInteractionFixture(t *testing.T, target string) *sharedInteractionFixture {
	t.Helper()
	root, child := createSharedInteractionTemplates(t, startSharedInteractionMock(t))
	return &sharedInteractionFixture{
		interactionFixture: newInteractionFixtureForTemplate(t, target, root),
		childTemplate:      child,
	}
}

func (f *interactionFixture) send(t *testing.T, text string) (*a2atype.Message, *a2apb.SendMessageRequest, *a2atype.Task) {
	t.Helper()
	message, request := newMessageRequest(t, text)
	response, err := f.client.SendMessage(f.ctx, request)
	if err != nil {
		t.Fatalf("send A2A message: %v", err)
	}
	result, err := pbconv.FromProtoSendMessageResponse(response)
	if err != nil {
		t.Fatalf("decode A2A response: %v", err)
	}
	task, ok := result.(*a2atype.Task)
	if !ok {
		t.Fatalf("A2A response = %T, want Task", result)
	}
	return message, request, task
}

func newMessageRequest(t *testing.T, text string) (*a2atype.Message, *a2apb.SendMessageRequest) {
	t.Helper()
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text))
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: message})
	if err != nil {
		t.Fatalf("build A2A request: %v", err)
	}
	return message, request
}

type streamReceiver interface {
	Recv() (*a2apb.StreamResponse, error)
}

func waitForTaskState(t *testing.T, stream streamReceiver, want a2atype.TaskState) {
	t.Helper()
	for {
		response, err := stream.Recv()
		if err != nil {
			t.Fatalf("receive task stream: %v", err)
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			t.Fatalf("decode task stream: %v", err)
		}
		switch event := event.(type) {
		case *a2atype.Task:
			if event.Status.State == want {
				return
			}
		case *a2atype.TaskStatusUpdateEvent:
			if event.Status.State == want {
				return
			}
		}
	}
}

func startInteractionMock(t *testing.T) string {
	return startMockLLM(t, "mocks/invoke_golang_adk_agent.json")
}

func startMockLLM(t *testing.T, fixture string) string {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile(fixture, interactionMocks)
	if err != nil {
		t.Fatalf("load mock LLM response: %v", err)
	}
	server := mockllm.NewServer(cfg)
	baseURL, err := server.Start(t.Context())
	if err != nil {
		t.Fatalf("start mock LLM: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(context.Background()); err != nil {
			t.Errorf("stop mock LLM: %v", err)
		}
	})
	return reachableModelURL(t, baseURL)
}

func startBlockingInteractionMock(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_golang_adk_agent.json", interactionMocks)
	if err != nil {
		t.Fatalf("load mock LLM response: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			return
		case <-release:
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cfg.OpenAI[0].Response); err != nil {
			t.Errorf("write mock LLM response: %v", err)
		}
	}))
	_ = server.Listener.Close()
	server.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for blocking mock LLM: %v", err)
	}
	server.Start()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	})
	return reachableModelURL(t, server.URL), started
}

func startSharedInteractionMock(t *testing.T) string {
	return startMockLLM(t, "mocks/invoke_shared_agent.json")
}

func startMCPMock(t *testing.T) (string, *mockmcp.Server) {
	t.Helper()
	server, err := mockmcp.NewServer(mockmcp.Options{Addr: "0.0.0.0:0", RecordRequests: true})
	if err != nil {
		t.Fatalf("create mock MCP server: %v", err)
	}
	baseURL, err := server.Start(t.Context())
	if err != nil {
		t.Fatalf("start mock MCP server: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(context.Background()); err != nil {
			t.Errorf("stop mock MCP server: %v", err)
		}
	})
	return reachableServerURL(t, baseURL, mockmcp.MCPPath), server
}

func reachableModelURL(t *testing.T, baseURL string) string {
	return reachableServerURL(t, baseURL, "/v1")
}

func reachableServerURL(t *testing.T, baseURL, path string) string {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse mock LLM URL: %v", err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("parse mock LLM address: %v", err)
	}
	host := os.Getenv("KAGENT_LOCAL_HOST")
	if host == "" {
		switch goruntime.GOOS {
		case "darwin":
			host = "host.docker.internal"
		case "linux":
			host = "172.17.0.1"
		default:
			t.Fatalf("KAGENT_LOCAL_HOST is required on %s", goruntime.GOOS)
		}
	}
	parsed.Host = net.JoinHostPort(host, port)
	parsed.Path = path
	return parsed.String()
}

func createInteractionTemplate(t *testing.T, modelURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	model := createInteractionModel(t, kube, modelURL, nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "interaction-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "kagent"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: model.Name},
			Description:  "Agent interaction E2E fixture",
			SystemPrompt: "Reply briefly.",
		},
	}
	createAndWaitInteractionTemplate(t, kube, template)
	return template.Name
}

func createMCPInteractionTemplate(t *testing.T, modelURL, mcpURL string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	model := createInteractionModel(t, kube, modelURL, nil)
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "interaction-mcp-", Namespace: "kagent"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Description: "MCP interaction E2E fixture",
			Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:         mcpURL,
		},
	}
	if err := kube.Create(t.Context(), server); err != nil {
		t.Fatalf("create interaction RemoteMCPServer: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interaction RemoteMCPServer: %v", err)
		}
	})
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-interaction-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "kagent"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: model.Name},
			Description:  "MCP interaction E2E fixture",
			SystemPrompt: "Use add_numbers to answer arithmetic questions.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: v1alpha3.AgentTemplateTypedLocalReference{Kind: "RemoteMCPServer", Name: server.Name},
				Tools:  []string{"add_numbers"},
			}}},
		},
	}
	createAndWaitInteractionTemplate(t, kube, template)
	return template.Name
}

func createSharedInteractionTemplates(t *testing.T, modelURL string) (string, string) {
	t.Helper()
	kube := interactionKubeClient(t)
	rootModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Agent": "root"})
	childModel := createInteractionModel(t, kube, modelURL, map[string]string{"X-Kagent-E2E-Agent": "child"})
	child := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "shared-child-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "kagent"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: childModel.Name},
			Description:  "Shared specialist",
			SystemPrompt: "Answer as the shared specialist.",
		},
	}
	createAndWaitInteractionTemplate(t, kube, child)
	root := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "shared-root-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "kagent"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: rootModel.Name},
			Description:  "Shared agent interaction E2E fixture",
			SystemPrompt: "Delegate every request to the specialist.",
			Tools: []v1alpha3.ToolBinding{{Agent: &v1alpha3.AgentToolBinding{
				Name: "specialist", Description: "Handles specialist requests",
				TemplateRef: v1alpha3.AgentTemplateLocalReference{Name: child.Name},
				Isolation:   v1alpha3.AgentToolIsolationShared,
			}}},
		},
	}
	createAndWaitInteractionTemplate(t, kube, root)
	return root.Name, child.Name
}

func interactionKubeClient(t *testing.T) ctrlclient.Client {
	t.Helper()
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("load Kubernetes config: %v", err)
	}
	clientScheme := k8sruntime.NewScheme()
	if err := v1alpha3.AddToScheme(clientScheme); err != nil {
		t.Fatalf("register kagent API: %v", err)
	}
	kube, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: clientScheme})
	if err != nil {
		t.Fatalf("create Kubernetes client: %v", err)
	}
	return kube
}

func createInteractionModel(t *testing.T, kube ctrlclient.Client, modelURL string, headers map[string]string) *v1alpha3.ModelConfig {
	t.Helper()
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "interaction-", Namespace: "kagent"},
		Spec: v1alpha3.ModelConfigSpec{
			Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4.1-mini",
			APIKeySecret: "kagent-openai", APIKeySecretKey: "OPENAI_API_KEY",
			OpenAI: &v1alpha3.OpenAIConfig{BaseURL: modelURL}, DefaultHeaders: headers,
		},
	}
	if err := kube.Create(t.Context(), model); err != nil {
		t.Fatalf("create interaction ModelConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), model); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interaction ModelConfig: %v", err)
		}
	})
	return model
}

func createAndWaitInteractionTemplate(t *testing.T, kube ctrlclient.Client, template *v1alpha3.AgentTemplate) {
	t.Helper()
	if err := kube.Create(t.Context(), template); err != nil {
		t.Fatalf("create interaction AgentTemplate: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), template); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete interaction AgentTemplate: %v", err)
		}
	})

	err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(template), template); err != nil {
			return false, err
		}
		for _, harness := range template.Status.Harnesses {
			if harness.Harness != "kagent" {
				continue
			}
			for _, condition := range harness.Conditions {
				if condition.Type == v1alpha3.AgentTemplateConditionReady && condition.Status == metav1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("wait for interaction AgentTemplate: %v", err)
	}
}

func taskText(task *a2atype.Task) string {
	var parts []string
	if task.Status.Message != nil {
		for _, part := range task.Status.Message.Parts {
			parts = append(parts, part.Text())
		}
	}
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			parts = append(parts, part.Text())
		}
	}
	return strings.Join(parts, "\n")
}
