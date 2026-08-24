package a2agateway

import (
	"context"
	"errors"
	"iter"
	"net"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const (
	gatewayTestID  = "8bd650a8-9775-488f-8bc1-0d52bf7bdcab"
	gatewayTestURL = "https://gateway.example"
)

type gatewayTestSession struct{}

func (gatewayTestSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: "alice"}}
}

type gatewayTestStore struct {
	instance              *apiv1alpha1.AgentInstance
	revision              *dbpkg.RuntimeRevision
	err                   error
	task                  *a2atype.Task
	tasks                 []*a2atype.Task
	total                 int
	taskErr               error
	replay                *a2atype.Task
	active                *a2atype.Task
	interruptResult       bool
	interrupted           bool
	stored                []a2atype.Event
	namespace, id, userID string
}

func (s *gatewayTestStore) GetAgentInstance(_ context.Context, namespace, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	s.namespace, s.id, s.userID = namespace, id, userID
	return s.instance, s.err
}

func (s *gatewayTestStore) GetRuntimeRevision(context.Context, string) (*dbpkg.RuntimeRevision, error) {
	return s.revision, nil
}

func (s *gatewayTestStore) StoreAgentInstanceTaskEvent(_ context.Context, _ string, task *a2atype.Task, event a2atype.Event) error {
	if s.taskErr != nil {
		return s.taskErr
	}
	s.task = task
	s.stored = append(s.stored, event)
	if task != nil && task.Status.State.Terminal() && s.active != nil && task.ID == s.active.ID {
		s.active = nil
	}
	return nil
}

func (s *gatewayTestStore) CreateAgentInstanceTask(_ context.Context, _ string, _ []byte, task *a2atype.Task) (*a2atype.Task, bool, error) {
	if s.taskErr != nil {
		return nil, false, s.taskErr
	}
	if s.replay != nil {
		return s.replay, false, nil
	}
	if s.active != nil {
		return nil, false, dbpkg.ErrAgentInstanceTaskConflict
	}
	s.task = task
	s.active = task
	s.stored = append(s.stored, task.History[0])
	return task, true, nil
}

func (s *gatewayTestStore) GetActiveAgentInstanceTask(context.Context, string) (*a2atype.Task, error) {
	if s.active == nil {
		return nil, dbpkg.ErrNotFound
	}
	return s.active, nil
}

func (s *gatewayTestStore) InterruptActiveAgentInstanceTask(_ context.Context, _ string, taskID string) (bool, error) {
	if !s.interruptResult || s.active == nil || string(s.active.ID) != taskID {
		return false, nil
	}
	s.active = nil
	s.interrupted = true
	return true, nil
}

func (s *gatewayTestStore) GetAgentInstanceTask(context.Context, string, string) (*a2atype.Task, error) {
	return s.task, s.taskErr
}

func (s *gatewayTestStore) ListAgentInstanceTasks(context.Context, string, string, a2atype.TaskState, *time.Time, int) ([]*a2atype.Task, int, error) {
	return s.tasks, s.total, s.taskErr
}

type gatewayTestAuthorizer struct {
	verb     auth.Verb
	resource auth.Resource
}

func (a *gatewayTestAuthorizer) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.verb, a.resource = verb, resource
	return nil
}

type gatewayTestDialer struct {
	client   *a2aclient.Client
	instance *apiv1alpha1.AgentInstance
	err      error
}

func (d *gatewayTestDialer) Dial(_ context.Context, instance *apiv1alpha1.AgentInstance) (*a2aclient.Client, error) {
	d.instance = instance
	return d.client, d.err
}

type gatewayTestRuntime struct {
	a2aclient.Transport
	sent           bool
	destroyed      bool
	task           *a2atype.Task
	taskErr        error
	taskResults    []*a2atype.Task
	getTaskCalls   int
	subscribeEvent a2atype.Event
	subscribeErr   error
}

func (r *gatewayTestRuntime) GetTask(context.Context, a2aclient.ServiceParams, *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	call := r.getTaskCalls
	r.getTaskCalls++
	if call < len(r.taskResults) {
		return r.taskResults[call], nil
	}
	return r.task, r.taskErr
}

func (r *gatewayTestRuntime) SubscribeToTask(context.Context, a2aclient.ServiceParams, *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if r.subscribeEvent != nil || r.subscribeErr != nil {
			yield(r.subscribeEvent, r.subscribeErr)
		}
	}
}

func (r *gatewayTestRuntime) SendMessage(_ context.Context, _ a2aclient.ServiceParams, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	r.sent = true
	return &a2atype.Task{ID: req.Message.TaskID, ContextID: req.Message.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}, nil
}

func (r *gatewayTestRuntime) SendStreamingMessage(_ context.Context, _ a2aclient.ServiceParams, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		yield(&a2atype.Task{ID: req.Message.TaskID, ContextID: req.Message.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}, nil)
	}
}

func (r *gatewayTestRuntime) Destroy() error {
	r.destroyed = true
	return nil
}

func gatewayTestClient(t *testing.T, runtime *gatewayTestRuntime) *a2aclient.Client {
	t.Helper()
	client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2atype.AgentInterface{{
		URL:             "runtime.test",
		ProtocolBinding: a2atype.TransportProtocolGRPC,
		ProtocolVersion: a2atype.Version,
	}},
		a2aclient.WithDefaultsDisabled(),
		a2aclient.WithTransport(a2atype.TransportProtocolGRPC, a2aclient.TransportFactoryFn(func(context.Context, *a2atype.AgentCard, *a2atype.AgentInterface) (a2aclient.Transport, error) {
			return runtime, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func gatewayTestContext() context.Context {
	return gatewayTestContextWithRoute("team-a", gatewayTestID)
}

func gatewayTestContextWithRoute(namespace, id string) context.Context {
	ctx := auth.AuthSessionTo(context.Background(), gatewayTestSession{})
	return metadata.NewIncomingContext(ctx, metadata.Pairs(
		AgentInstanceNamespaceHeader, namespace,
		AgentInstanceIDHeader, id,
	))
}

func gatewayTestInstance() *apiv1alpha1.AgentInstance {
	return &apiv1alpha1.AgentInstance{
		Id: gatewayTestID, Namespace: "team-a", Creator: "alice",
		PreparedRevision: "revision-1",
		A2AAuthority:     "private-runtime-authority",
		State:            apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
	}
}

func gatewayTestRequest() *a2atype.SendMessageRequest {
	return &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))}
}

func TestGatewayResolvesAuthenticatedHeadersBeforeSending(t *testing.T) {
	instance := gatewayTestInstance()
	store := &gatewayTestStore{instance: instance}
	authorizer := &gatewayTestAuthorizer{}
	runtime := &gatewayTestRuntime{}
	gateway := New(store, authorizer, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)

	result, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.(*a2atype.Task).ID == "" || !runtime.sent || !runtime.destroyed {
		t.Fatalf("runtime result = %#v, sent %v, destroyed %v", result, runtime.sent, runtime.destroyed)
	}
	if store.namespace != "team-a" || store.id != gatewayTestID || store.userID != "alice" {
		t.Fatalf("store lookup = %q/%q user %q", store.namespace, store.id, store.userID)
	}
	if authorizer.verb != auth.VerbCreate || authorizer.resource != (auth.Resource{Type: "AgentInstance", Name: "team-a/" + gatewayTestID}) {
		t.Fatalf("authorization = %q %#v", authorizer.verb, authorizer.resource)
	}
}

func TestGatewayClosesRuntimeAfterStreaming(t *testing.T) {
	instance := gatewayTestInstance()
	runtime := &gatewayTestRuntime{}
	gateway := New(&gatewayTestStore{instance: instance}, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)

	var events int
	for _, err := range gateway.SendStreamingMessage(gatewayTestContext(), gatewayTestRequest()) {
		if err != nil {
			t.Fatal(err)
		}
		events++
	}
	if events != 1 || !runtime.destroyed {
		t.Fatalf("stream events = %d, destroyed %v", events, runtime.destroyed)
	}
}

func TestGatewayRequiresValidRoutingHeaders(t *testing.T) {
	gateway := New(&gatewayTestStore{instance: gatewayTestInstance()}, &gatewayTestAuthorizer{}, &gatewayTestDialer{}, gatewayTestURL)
	for _, ctx := range []context.Context{
		auth.AuthSessionTo(context.Background(), gatewayTestSession{}),
		gatewayTestContextWithRoute("INVALID", gatewayTestID),
		gatewayTestContextWithRoute("team-a", "not-a-uuid"),
	} {
		if _, err := gateway.SendMessage(ctx, &a2atype.SendMessageRequest{}); err == nil {
			t.Fatal("SendMessage() accepted invalid routing headers")
		}
	}
}

func TestGatewayHidesInternalErrors(t *testing.T) {
	instance := gatewayTestInstance()
	for _, test := range []struct {
		name    string
		store   *gatewayTestStore
		dialer  *gatewayTestDialer
		message string
	}{
		{name: "store", store: &gatewayTestStore{err: errors.New("password=secret")}, dialer: &gatewayTestDialer{}, message: "failed to load AgentInstance"},
		{name: "dialer", store: &gatewayTestStore{instance: instance}, dialer: &gatewayTestDialer{err: errors.New("internal.host:1234")}, message: "failed to connect to AgentInstance runtime"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := New(test.store, &gatewayTestAuthorizer{}, test.dialer, gatewayTestURL)
			_, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest())
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("SendMessage() error = %v, want %q", err, test.message)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "internal.host") {
				t.Fatalf("SendMessage() leaked internal error: %v", err)
			}
		})
	}
}

func TestGatewayReadsRoutingHeadersFromGRPC(t *testing.T) {
	instance := gatewayTestInstance()
	runtime := &gatewayTestRuntime{}
	gateway := New(&gatewayTestStore{instance: instance}, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(auth.AuthSessionTo(ctx, gatewayTestSession{}), req)
	}))
	a2agrpc.NewHandler(gateway).RegisterWith(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello")),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(
		AgentInstanceNamespaceHeader, instance.GetNamespace(),
		AgentInstanceIDHeader, instance.GetId(),
	))
	if _, err := a2apb.NewA2AServiceClient(connection).SendMessage(ctx, request); err != nil {
		t.Fatal(err)
	}
	if !runtime.sent {
		t.Fatal("gRPC request did not reach the AgentInstance runtime")
	}
}

func TestRuntimeDialerRequiresAuthority(t *testing.T) {
	if _, err := (&RuntimeDialer{}).Dial(t.Context(), &apiv1alpha1.AgentInstance{}); err == nil {
		t.Fatal("Dial() accepted an empty runtime authority")
	}
}

func TestGatewayReadsTasksWithoutDialingRuntime(t *testing.T) {
	task := &a2atype.Task{
		ID: gatewayTestID, ContextID: gatewayTestID,
		History:   []*a2atype.Message{{ID: "one"}, {ID: "two"}},
		Artifacts: []*a2atype.Artifact{{Name: "result"}},
	}
	store := &gatewayTestStore{instance: gatewayTestInstance(), task: task, tasks: []*a2atype.Task{task}, total: 1}
	dialer := &gatewayTestDialer{}
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, gatewayTestURL)
	historyLength := 1

	got, err := gateway.GetTask(gatewayTestContext(), &a2atype.GetTaskRequest{ID: task.ID, HistoryLength: &historyLength})
	if err != nil || len(got.History) != 1 || len(got.Artifacts) != 1 {
		t.Fatalf("GetTask() = %#v, %v", got, err)
	}
	listed, err := gateway.ListTasks(gatewayTestContext(), &a2atype.ListTasksRequest{HistoryLength: &historyLength})
	if err != nil || len(listed.Tasks) != 1 || len(listed.Tasks[0].History) != 1 || listed.Tasks[0].Artifacts != nil {
		t.Fatalf("ListTasks() = %#v, %v", listed, err)
	}
	if dialer.instance != nil {
		t.Fatal("task reads dialed the private runtime")
	}
}

func TestGatewayBuildsAgentCardFromPinnedRevision(t *testing.T) {
	store := &gatewayTestStore{
		instance: gatewayTestInstance(),
		revision: &dbpkg.RuntimeRevision{
			Revision: "revision-1",
			AgentCard: []byte(`{
				"name":"assistant","description":"pinned description","version":"v1",
				"supportedInterfaces":[{"url":"http://127.0.0.1:80","protocolBinding":"GRPC","protocolVersion":"1.0"}],
				"capabilities":{"pushNotifications":true},"skills":[],
				"defaultInputModes":["text"],"defaultOutputModes":["text"]
			}`),
		},
	}
	authorizer := &gatewayTestAuthorizer{}
	dialer := &gatewayTestDialer{}
	gateway := New(store, authorizer, dialer, gatewayTestURL)

	card, err := gateway.GetExtendedAgentCard(gatewayTestContext(), &a2atype.GetExtendedAgentCardRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "assistant" || card.Description != "pinned description" || card.Version != "v1" {
		t.Fatalf("template metadata = %#v", card)
	}
	if len(card.SupportedInterfaces) != 1 || card.SupportedInterfaces[0].URL != gatewayTestURL ||
		card.SupportedInterfaces[0].ProtocolBinding != a2atype.TransportProtocolGRPC {
		t.Fatalf("public interfaces = %#v", card.SupportedInterfaces)
	}
	if !card.Capabilities.Streaming || !card.Capabilities.ExtendedAgentCard || card.Capabilities.PushNotifications {
		t.Fatalf("gateway capabilities = %#v", card.Capabilities)
	}
	if authorizer.verb != auth.VerbGet || dialer.instance != nil {
		t.Fatalf("authorization verb = %q, runtime dialed = %v", authorizer.verb, dialer.instance != nil)
	}
}

func TestGatewayPersistsBeforePublishing(t *testing.T) {
	store := &gatewayTestStore{instance: gatewayTestInstance()}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, &gatewayTestRuntime{})}, gatewayTestURL)

	for _, err := range gateway.SendStreamingMessage(gatewayTestContext(), gatewayTestRequest()) {
		if err != nil {
			t.Fatal(err)
		}
		if len(store.stored) != 2 {
			t.Fatalf("published event after %d durable writes, want 2", len(store.stored))
		}
	}
}

func TestGatewayKeepsLiveRuntimeTaskBusy(t *testing.T) {
	active := &a2atype.Task{ID: "active", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	runtime := &gatewayTestRuntime{task: active, subscribeEvent: active}
	store := &gatewayTestStore{instance: gatewayTestInstance(), active: active}
	dialer := &gatewayTestDialer{client: gatewayTestClient(t, runtime)}
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, gatewayTestURL)

	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err == nil {
		t.Fatal("SendMessage() accepted a second active task")
	}
	if dialer.instance == nil || runtime.sent || store.interrupted || runtime.getTaskCalls != 0 {
		t.Fatalf("live task reconciliation: dialed=%v sent=%v interrupted=%v GetTask calls=%d", dialer.instance != nil, runtime.sent, store.interrupted, runtime.getTaskCalls)
	}
}

func TestGatewayInterruptsTaskWithoutRuntimeExecution(t *testing.T) {
	active := &a2atype.Task{ID: "active", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	runtime := &gatewayTestRuntime{taskResults: []*a2atype.Task{active}, subscribeErr: a2atype.ErrTaskNotFound}
	store := &gatewayTestStore{instance: gatewayTestInstance(), active: active, interruptResult: true}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)

	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err != nil {
		t.Fatal(err)
	}
	if !store.interrupted || !runtime.sent || runtime.getTaskCalls != 1 {
		t.Fatalf("orphan reconciliation: interrupted=%v sent=%v GetTask calls=%d", store.interrupted, runtime.sent, runtime.getTaskCalls)
	}
}

func TestGatewayDoesNotInterruptTaskBeforeRuntimeDispatch(t *testing.T) {
	active := &a2atype.Task{ID: "active", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateSubmitted}}
	runtime := &gatewayTestRuntime{taskErr: a2atype.ErrTaskNotFound, subscribeErr: a2atype.ErrTaskNotFound}
	store := &gatewayTestStore{instance: gatewayTestInstance(), active: active, interruptResult: true}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)

	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err == nil {
		t.Fatal("SendMessage() replaced a task that had not reached the runtime")
	}
	if store.interrupted || runtime.sent || runtime.getTaskCalls != 1 {
		t.Fatalf("pre-dispatch task: interrupted=%v sent replacement=%v GetTask calls=%d", store.interrupted, runtime.sent, runtime.getTaskCalls)
	}
}

func TestGatewayPersistsTerminalRuntimeTaskBeforeRetry(t *testing.T) {
	active := &a2atype.Task{ID: "active", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	terminal := &a2atype.Task{ID: active.ID, ContextID: active.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}
	runtime := &gatewayTestRuntime{taskResults: []*a2atype.Task{terminal}, subscribeErr: a2atype.ErrTaskNotFound}
	store := &gatewayTestStore{instance: gatewayTestInstance(), active: active}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)

	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err != nil {
		t.Fatal(err)
	}
	if len(store.stored) < 2 || store.stored[0] != terminal || !runtime.sent || runtime.getTaskCalls != 1 {
		t.Fatalf("terminal reconciliation: stored=%#v sent=%v GetTask calls=%d", store.stored, runtime.sent, runtime.getTaskCalls)
	}
}

func TestGatewayDoesNotInterruptWhenRuntimeIsUnavailable(t *testing.T) {
	active := &a2atype.Task{ID: "active", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	store := &gatewayTestStore{instance: gatewayTestInstance(), active: active, interruptResult: true}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{err: errors.New("runtime unavailable")}, gatewayTestURL)

	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err == nil {
		t.Fatal("SendMessage() accepted a second task while runtime state was unknown")
	}
	if store.interrupted {
		t.Fatal("unavailable runtime task was interrupted")
	}
}

func TestGatewayReplaysDuplicateMessageWithoutDialing(t *testing.T) {
	existing := &a2atype.Task{ID: "existing-task", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}
	store := &gatewayTestStore{instance: gatewayTestInstance(), replay: existing}
	dialer := &gatewayTestDialer{}
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, gatewayTestURL)

	result, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest())
	if err != nil || result != existing {
		t.Fatalf("SendMessage() = %#v, %v", result, err)
	}
	if dialer.instance != nil {
		t.Fatal("duplicate message dialed the private runtime")
	}
}

func TestGatewayRejectsConflictingMessageIDWithoutDialing(t *testing.T) {
	store := &gatewayTestStore{instance: gatewayTestInstance(), taskErr: dbpkg.ErrIdempotencyConflict}
	dialer := &gatewayTestDialer{}
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, gatewayTestURL)

	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err == nil {
		t.Fatal("SendMessage() accepted a reused message ID with different content")
	}
	if dialer.instance != nil {
		t.Fatal("conflicting message dialed the private runtime")
	}
}
