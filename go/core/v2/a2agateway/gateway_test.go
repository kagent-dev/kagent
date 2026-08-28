package a2agateway

import (
	"context"
	"errors"
	"iter"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
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
	created               *a2atype.Task
	tasks                 []*a2atype.Task
	total                 int
	taskErr               error
	replay                *a2atype.Task
	active                *a2atype.Task
	interruptResult       bool
	interrupted           bool
	createdTasks          int
	stored                []a2atype.Event
	snapshot              *dbpkg.AgentInstanceTaskSnapshot
	onStore               func()
	namespace, id, userID string
}

func (s *gatewayTestStore) GetAgentInstance(_ context.Context, namespace, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	s.namespace, s.id, s.userID = namespace, id, userID
	return s.instance, s.err
}

func (s *gatewayTestStore) GetRuntimeRevision(context.Context, string) (*dbpkg.RuntimeRevision, error) {
	return s.revision, nil
}

func (s *gatewayTestStore) StoreAgentInstanceTaskEvent(_ context.Context, _ string, task *a2atype.Task, event a2atype.Event, snapshot *dbpkg.AgentInstanceTaskSnapshot) error {
	if s.taskErr != nil {
		return s.taskErr
	}
	if s.onStore != nil {
		s.onStore()
	}
	s.task = task
	s.snapshot = snapshot
	s.stored = append(s.stored, event)
	if task != nil && isQuiescent(task.Status.State) && s.active != nil && task.ID == s.active.ID {
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
	s.created = task
	s.active = task
	s.createdTasks++
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

func (s *gatewayTestStore) GetAgentInstanceTask(_ context.Context, _ string, taskID string) (*a2atype.Task, error) {
	if s.taskErr != nil {
		return nil, s.taskErr
	}
	if s.task == nil || string(s.task.ID) != taskID {
		return nil, dbpkg.ErrNotFound
	}
	return s.task, nil
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

type gatewayTestWorkflow struct {
	quiesceCalls int
	err          error
	onQuiesce    func()
}

func (w *gatewayTestWorkflow) Quiesce(context.Context, *apiv1alpha1.AgentInstance) (*dbpkg.AgentInstanceTaskSnapshot, error) {
	w.quiesceCalls++
	if w.onQuiesce != nil {
		w.onQuiesce()
	}
	return &dbpkg.AgentInstanceTaskSnapshot{Atespace: "team-a", Name: "snapshot-1", UID: "snapshot-uid"}, w.err
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
	privateTask    *a2atype.Task
	subscribeCalls int
	cancelErr      error
	sendCalls      int
	sentTaskID     a2atype.TaskID
}

func (r *gatewayTestRuntime) CancelTask(context.Context, a2aclient.ServiceParams, *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	if r.cancelErr != nil {
		return nil, r.cancelErr
	}
	return r.task, nil
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
	r.subscribeCalls++
	return func(yield func(a2atype.Event, error) bool) {
		if r.subscribeEvent != nil || r.subscribeErr != nil {
			yield(r.subscribeEvent, r.subscribeErr)
		}
	}
}

func (r *gatewayTestRuntime) SendMessage(_ context.Context, _ a2aclient.ServiceParams, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	r.sent = true
	r.privateTask, _ = apia2a.TakeStoredTask(req.Message)
	r.sendCalls++
	r.sentTaskID = req.Message.TaskID
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

func gatewayTestClient(t *testing.T, runtime a2aclient.Transport) *a2aclient.Client {
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

type blockingGatewayRuntime struct {
	a2aclient.Transport
	started       chan struct{}
	canceled      chan struct{}
	releaseCancel chan struct{}
	once          sync.Once

	mu   sync.Mutex
	task *a2atype.Task
}

func (r *blockingGatewayRuntime) SendStreamingMessage(_ context.Context, _ a2aclient.ServiceParams, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		task := &a2atype.Task{ID: req.Message.TaskID, ContextID: req.Message.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
		r.mu.Lock()
		r.task = task
		r.mu.Unlock()
		close(r.started)
		<-r.canceled
		yield(a2atype.NewStatusUpdateEvent(task, a2atype.TaskStateCanceled, nil), nil)
	}
}

func (r *blockingGatewayRuntime) CancelTask(_ context.Context, _ a2aclient.ServiceParams, _ *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	r.mu.Lock()
	task := *r.task
	r.mu.Unlock()
	task.Status.State = a2atype.TaskStateCanceled
	r.once.Do(func() { close(r.canceled) })
	<-r.releaseCancel
	return &task, nil
}

func (r *blockingGatewayRuntime) Destroy() error { return nil }

type gatewayTestCoordinator struct {
	runtimeCoordinator
	quiescing chan struct{}
}

func (c *gatewayTestCoordinator) Quiesce(instanceID string) func() {
	close(c.quiescing)
	return c.runtimeCoordinator.Quiesce(instanceID)
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
	gateway := New(store, authorizer, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
	request := gatewayTestRequest()
	request.Message.SetMeta(apia2a.TimelinePositionMetadataKey, "caller-controlled")

	result, err := gateway.SendMessage(gatewayTestContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.(*a2atype.Task).ID == "" || !runtime.sent || !runtime.destroyed {
		t.Fatalf("runtime result = %#v, sent %v, destroyed %v", result, runtime.sent, runtime.destroyed)
	}
	createdAt, ok := store.created.Metadata[TaskCreatedAtMetadataKey].(string)
	if _, err := time.Parse(time.RFC3339Nano, createdAt); !ok || err != nil {
		t.Fatalf("task creation timestamp = %#v: %v", store.created.Metadata[TaskCreatedAtMetadataKey], err)
	}
	position, ok := store.created.History[0].Metadata[apia2a.TimelinePositionMetadataKey].(string)
	if _, err := time.Parse(time.RFC3339Nano, position); !ok || err != nil {
		t.Fatalf("opening message timeline position = %#v: %v", store.created.History[0].Metadata[apia2a.TimelinePositionMetadataKey], err)
	}
	if store.namespace != "team-a" || store.id != gatewayTestID || store.userID != "alice" {
		t.Fatalf("store lookup = %q/%q user %q", store.namespace, store.id, store.userID)
	}
	if authorizer.verb != auth.VerbCreate || authorizer.resource != (auth.Resource{Type: "AgentInstance", Name: "team-a/" + gatewayTestID}) {
		t.Fatalf("authorization = %q %#v", authorizer.verb, authorizer.resource)
	}
}

func TestGatewayReturnsStoredTaskIDOnReplay(t *testing.T) {
	replayed := &a2atype.Task{
		ID: "replayed-task", ContextID: gatewayTestID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
	}
	request := gatewayTestRequest()
	gateway := New(
		&gatewayTestStore{instance: gatewayTestInstance(), replay: replayed},
		&gatewayTestAuthorizer{}, &gatewayTestDialer{}, &gatewayTestWorkflow{}, gatewayTestURL,
	)
	for _, err := range gateway.SendStreamingMessage(gatewayTestContext(), request) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if request.Message.TaskID != replayed.ID {
		t.Fatalf("request task ID = %q, want stored ID %q", request.Message.TaskID, replayed.ID)
	}
}

func TestGatewayContinuesInputRequiredTask(t *testing.T) {
	status := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("waiting"))
	waiting := &a2atype.Task{
		ID: "task-1", ContextID: gatewayTestID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: status},
	}
	store := &gatewayTestStore{instance: gatewayTestInstance(), task: waiting}
	runtime := &gatewayTestRuntime{}
	authorizer := &gatewayTestAuthorizer{}
	gateway := New(store, authorizer, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
	reply := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("PostgreSQL"))
	reply.TaskID = waiting.ID
	reply.Metadata = map[string]any{"https://kagent.dev/internal/stored-task/v1": "untrusted"}

	result, err := gateway.SendMessage(gatewayTestContext(), &a2atype.SendMessageRequest{Message: reply})
	if err != nil {
		t.Fatal(err)
	}
	task, ok := result.(*a2atype.Task)
	if !ok || task.Status.State != a2atype.TaskStateCompleted || !runtime.sent {
		t.Fatalf("reply result = %#v, runtime sent = %v", result, runtime.sent)
	}
	if authorizer.verb != auth.VerbUpdate || reply.ContextID != gatewayTestID || len(store.stored) != 2 {
		t.Fatalf("reply authorization = %s, context = %q, stored events = %d", authorizer.verb, reply.ContextID, len(store.stored))
	}
	if runtime.privateTask == nil || runtime.privateTask.Status.State != a2atype.TaskStateInputRequired || runtime.privateTask.Status.Message == nil || runtime.privateTask.Status.Message.ID != status.ID {
		t.Fatalf("private continuation state = %#v", runtime.privateTask)
	}
	position, ok := reply.Metadata[apia2a.TimelinePositionMetadataKey].(string)
	if _, err := time.Parse(time.RFC3339Nano, position); !ok || err != nil || len(reply.Metadata) != 1 {
		t.Fatalf("reply metadata = %#v, want only its server-authored timeline position: %v", reply.Metadata, err)
	}
}

func TestGatewayMovesInputRequiredMessageBeforeReply(t *testing.T) {
	question := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Which database?"))
	waiting := &a2atype.Task{
		ID: "task-1", ContextID: gatewayTestID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: question},
	}
	reply := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("PostgreSQL"))
	reply.TaskID = waiting.ID
	store := &gatewayTestStore{task: waiting}
	gateway := &Gateway{store: store}

	prepared, err := gateway.prepareReply(t.Context(), gatewayTestInstance(), &a2atype.SendMessageRequest{Message: reply})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.task.History) != 2 || prepared.task.History[0] != question || prepared.task.History[1] != reply {
		t.Fatalf("history = %#v, want question followed by reply", prepared.task.History)
	}
	if len(store.stored) != 1 || store.stored[0] != reply {
		t.Fatalf("stored events = %#v, want one atomic reply update", store.stored)
	}
	if question.TaskID != waiting.ID || question.ContextID != waiting.ContextID {
		t.Fatalf("archived question = task %q context %q, want the task it was asked in", question.TaskID, question.ContextID)
	}
}

func TestGatewayRejectsInputRequiredMessageWithoutID(t *testing.T) {
	waiting := &a2atype.Task{
		ID: "task-1", ContextID: gatewayTestID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: &a2atype.Message{}},
	}
	store := &gatewayTestStore{task: waiting}
	gateway := &Gateway{store: store}
	reply := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("PostgreSQL"))
	reply.TaskID = waiting.ID

	if _, err := gateway.prepareReply(t.Context(), gatewayTestInstance(), &a2atype.SendMessageRequest{Message: reply}); err == nil {
		t.Fatal("prepareReply() succeeded with an unidentifiable status message")
	}
	if len(store.stored) != 0 {
		t.Fatalf("stored events = %#v, want no partial write", store.stored)
	}
}

func TestGatewayClosesRuntimeAfterStreaming(t *testing.T) {
	instance := gatewayTestInstance()
	runtime := &gatewayTestRuntime{}
	destroyedAtQuiesce := false
	workflow := &gatewayTestWorkflow{onQuiesce: func() { destroyedAtQuiesce = runtime.destroyed }}
	gateway := New(&gatewayTestStore{instance: instance}, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, workflow, gatewayTestURL)

	var events int
	for _, err := range gateway.SendStreamingMessage(gatewayTestContext(), gatewayTestRequest()) {
		if err != nil {
			t.Fatal(err)
		}
		events++
	}
	if events != 2 || !runtime.destroyed || !destroyedAtQuiesce {
		t.Fatalf("stream events = %d, destroyed %v, destroyed at quiesce %v", events, runtime.destroyed, destroyedAtQuiesce)
	}
}

func TestTaskForResultPreservesCreationTime(t *testing.T) {
	submitted := &a2atype.Task{
		ID: gatewayTestID, ContextID: gatewayTestID,
		Metadata: map[string]any{TaskCreatedAtMetadataKey: "2026-08-26T10:00:00Z"},
	}
	result, err := taskForResult(submitted, &a2atype.Task{
		ID: gatewayTestID, ContextID: gatewayTestID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata[TaskCreatedAtMetadataKey] != submitted.Metadata[TaskCreatedAtMetadataKey] {
		t.Fatalf("creation timestamp = %#v", result.Metadata[TaskCreatedAtMetadataKey])
	}
}

func TestTaskForEventFoldsStatusIntoLocalProjection(t *testing.T) {
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("canceled"))
	task := &a2atype.Task{
		ID: gatewayTestID, ContextID: gatewayTestID,
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateWorking},
		Artifacts: []*a2atype.Artifact{{Name: "partial"}},
	}
	event := a2atype.NewStatusUpdateEvent(task, a2atype.TaskStateCanceled, message)

	updated, err := taskForEvent(task, event)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status.State != a2atype.TaskStateCanceled || updated.Status.Message != message || len(updated.Artifacts) != 1 {
		t.Fatalf("updated task = %#v", updated)
	}
	if task.Status.State != a2atype.TaskStateWorking {
		t.Fatalf("input task state = %s, want WORKING", task.Status.State)
	}
}

func TestGatewayRequiresValidRoutingHeaders(t *testing.T) {
	gateway := New(&gatewayTestStore{instance: gatewayTestInstance()}, &gatewayTestAuthorizer{}, &gatewayTestDialer{}, &gatewayTestWorkflow{}, gatewayTestURL)
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
			gateway := New(test.store, &gatewayTestAuthorizer{}, test.dialer, &gatewayTestWorkflow{}, gatewayTestURL)
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
	gateway := New(&gatewayTestStore{instance: instance}, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)
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
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, &gatewayTestWorkflow{}, gatewayTestURL)
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

/*
 * A suspended conversation is still readable, and sending to it is still refused.
 *
 * Both halves matter and they used to be one rule. Every RPC resolved the instance
 * through a helper that insisted on READY, which is right for anything needing the
 * worker and wrong for a task list — that comes out of the store, which does not care
 * whether a worker is attached.
 *
 * It became a real fault once conversations started giving their workers back at the
 * end of every turn: opening one to re-read what was said reported "AgentInstance is
 * AGENT_INSTANCE_STATE_SUSPENDED" as though the transcript had been lost. Resuming on
 * open would have claimed a worker every time somebody glanced at one, which is the
 * thing suspending them exists to avoid.
 */
func TestGatewayReadsTasksWhileSuspended(t *testing.T) {
	instance := gatewayTestInstance()
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
	task := &a2atype.Task{ID: gatewayTestID, ContextID: gatewayTestID}
	store := &gatewayTestStore{instance: instance, task: task, tasks: []*a2atype.Task{task}, total: 1}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{}, &gatewayTestWorkflow{}, gatewayTestURL)

	if _, err := gateway.ListTasks(gatewayTestContext(), &a2atype.ListTasksRequest{}); err != nil {
		t.Fatalf("ListTasks() on a suspended instance = %v, want the stored transcript", err)
	}
	if _, err := gateway.GetTask(gatewayTestContext(), &a2atype.GetTaskRequest{ID: task.ID}); err != nil {
		t.Fatalf("GetTask() on a suspended instance = %v, want the stored task", err)
	}

	// The other half: what needs the worker is still refused, naming the state. Without
	// this the change would read as "suspended no longer means anything".
	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err == nil {
		t.Fatal("SendMessage() to a suspended instance succeeded, want a refusal naming the state")
	}
}

/*
 * A runtime that has forgotten the conversation must not erase it.
 *
 * `ApplyUpdate` takes the runtime's task where one is sent, which is right for status
 * and artifacts and wrong for history. A runtime that has been quiesced and resumed can
 * answer with a task carrying no history at all — and persisting that replaces the
 * transcript with an empty one, so the conversation opens blank on the next read while
 * its events sit untouched in the store beside it.
 *
 * That is what a conversation parked on a question did after being answered: an
 * eighty-byte task in place of everything that had been said.
 */
func TestGatewayKeepsHistoryARuntimeHasForgotten(t *testing.T) {
	stored := &a2atype.Task{
		ID: gatewayTestID, ContextID: gatewayTestID,
		History: []*a2atype.Message{{ID: "one"}, {ID: "two"}, {ID: "three"}},
	}
	forgetful := &a2atype.Task{
		ID: gatewayTestID, ContextID: gatewayTestID,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateFailed},
	}

	updated, err := taskForEvent(stored, forgetful)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.History) != len(stored.History) {
		t.Fatalf("history = %d messages, want the stored %d kept", len(updated.History), len(stored.History))
	}
	// The rest of the runtime's answer is still believed — this keeps history, it does
	// not ignore the update.
	if updated.Status.State != a2atype.TaskStateFailed {
		t.Fatalf("state = %s, want the runtime's %s", updated.Status.State, a2atype.TaskStateFailed)
	}

	// And a runtime with more to say is believed about that too, or an agent could
	// never add to a transcript at all.
	richer := &a2atype.Task{
		ID: gatewayTestID, ContextID: gatewayTestID,
		History: []*a2atype.Message{{ID: "one"}, {ID: "two"}, {ID: "three"}, {ID: "four"}},
	}
	grown, err := taskForEvent(stored, richer)
	if err != nil || len(grown.History) != 4 {
		t.Fatalf("history = %d messages (%v), want the runtime's 4", len(grown.History), err)
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
				"capabilities":{"pushNotifications":true,"extensions":[{"uri":"https://kagent.dev/extensions/hitl/v1","required":false}]},"skills":[],
				"defaultInputModes":["text"],"defaultOutputModes":["text"]
			}`),
		},
	}
	authorizer := &gatewayTestAuthorizer{}
	dialer := &gatewayTestDialer{}
	gateway := New(store, authorizer, dialer, &gatewayTestWorkflow{}, gatewayTestURL)

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
	// Transport and streaming are the gateway's to state, but extensions describe
	// what the runtime can negotiate. Replacing the whole struct used to drop them,
	// which left a client no way to discover that an agent's question is answerable
	// while the card still looked complete.
	if len(card.Capabilities.Extensions) != 1 || card.Capabilities.Extensions[0].URI != "https://kagent.dev/extensions/hitl/v1" {
		t.Fatalf("runtime extensions = %#v, want the runtime's own preserved", card.Capabilities.Extensions)
	}
	if authorizer.verb != auth.VerbGet || dialer.instance != nil {
		t.Fatalf("authorization verb = %q, runtime dialed = %v", authorizer.verb, dialer.instance != nil)
	}
}

func TestGatewayPersistsBeforePublishing(t *testing.T) {
	var order []string
	store := &gatewayTestStore{instance: gatewayTestInstance(), onStore: func() { order = append(order, "store") }}
	workflow := &gatewayTestWorkflow{onQuiesce: func() { order = append(order, "suspend") }}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, &gatewayTestRuntime{})}, workflow, gatewayTestURL)

	for event, err := range gateway.SendStreamingMessage(gatewayTestContext(), gatewayTestRequest()) {
		if err != nil {
			t.Fatal(err)
		}
		if task, ok := event.(*a2atype.Task); ok && task.Status.State == a2atype.TaskStateSubmitted {
			continue
		}
		if len(store.stored) != 2 {
			t.Fatalf("published event after %d durable writes, want 2", len(store.stored))
		}
		order = append(order, "publish")
	}
	if got := strings.Join(order, ","); got != "suspend,store,publish" {
		t.Fatalf("terminal event order = %q", got)
	}
	if workflow.quiesceCalls != 1 || store.snapshot == nil || store.snapshot.UID != "snapshot-uid" {
		t.Fatalf("quiescence calls = %d, stored snapshot = %#v", workflow.quiesceCalls, store.snapshot)
	}
}

func TestGatewayTaskRunOwnsTerminalEventSideEffects(t *testing.T) {
	runtime := &blockingGatewayRuntime{started: make(chan struct{}), canceled: make(chan struct{}), releaseCancel: make(chan struct{})}
	store := &gatewayTestStore{instance: gatewayTestInstance()}
	quiesced := make(chan struct{})
	workflow := &gatewayTestWorkflow{onQuiesce: func() { close(quiesced) }}
	coordinator := &gatewayTestCoordinator{runtimeCoordinator: &memoryRuntimeCoordinator{}, quiescing: make(chan struct{})}
	gateway := newGateway(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, workflow, gatewayTestURL, coordinator)

	stream := gateway.SendStreamingMessage(gatewayTestContext(), gatewayTestRequest())
	streamResult := make(chan a2atype.TaskState, 1)
	go collectTerminalState(stream, streamResult)
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("runtime stream did not start")
	}
	task := store.task
	subscription := gateway.SubscribeToTask(gatewayTestContext(), &a2atype.SubscribeToTaskRequest{ID: task.ID})

	subscriptionStarted := make(chan struct{})
	subscriptionResult := make(chan a2atype.TaskState, 1)
	go func() {
		first := true
		for event, err := range subscription {
			if err != nil {
				subscriptionResult <- a2atype.TaskStateUnspecified
				return
			}
			if first {
				close(subscriptionStarted)
				first = false
			}
			if task, ok := event.(*a2atype.Task); ok && task.Status.State.Terminal() {
				subscriptionResult <- task.Status.State
				return
			}
			if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State.Terminal() {
				subscriptionResult <- update.Status.State
				return
			}
		}
	}()
	select {
	case <-subscriptionStarted:
	case <-time.After(time.Second):
		t.Fatal("task subscription did not start")
	}

	type cancelResult struct {
		task *a2atype.Task
		err  error
	}
	cancelResultCh := make(chan cancelResult, 1)
	go func() {
		canceled, err := gateway.CancelTask(gatewayTestContext(), &a2atype.CancelTaskRequest{ID: task.ID})
		cancelResultCh <- cancelResult{task: canceled, err: err}
	}()
	select {
	case <-coordinator.quiescing:
	case <-time.After(time.Second):
		t.Fatal("terminal event did not request quiescence")
	}
	select {
	case <-quiesced:
		t.Fatal("quiescence started before cancellation returned")
	default:
	}
	close(runtime.releaseCancel)
	result := <-cancelResultCh
	canceled, err := result.task, result.err
	if err != nil || canceled.Status.State != a2atype.TaskStateCanceled {
		t.Fatalf("CancelTask() = %#v, %v", canceled, err)
	}
	for name, result := range map[string]<-chan a2atype.TaskState{"send stream": streamResult, "subscription": subscriptionResult} {
		select {
		case state := <-result:
			if state != a2atype.TaskStateCanceled {
				t.Fatalf("%s state = %s, want CANCELED", name, state)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive cancellation", name)
		}
	}
	if workflow.quiesceCalls != 1 || len(store.stored) != 2 {
		t.Fatalf("quiescence calls = %d, stored events = %d; want 1 and 2", workflow.quiesceCalls, len(store.stored))
	}
}

func TestGatewaySubscriptionRecoversTaskRun(t *testing.T) {
	active := &a2atype.Task{ID: "active", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	runtime := &gatewayTestRuntime{subscribeEvent: a2atype.NewStatusUpdateEvent(active, a2atype.TaskStateCanceled, nil)}
	store := &gatewayTestStore{instance: gatewayTestInstance(), task: active, active: active}
	workflow := &gatewayTestWorkflow{}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, workflow, gatewayTestURL)

	var events []a2atype.Event
	for event, err := range gateway.SubscribeToTask(gatewayTestContext(), &a2atype.SubscribeToTaskRequest{ID: active.ID}) {
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) != 2 || workflow.quiesceCalls != 1 || len(store.stored) != 1 || !runtime.destroyed {
		t.Fatalf("events = %d, quiescence calls = %d, stored events = %d, runtime destroyed = %v", len(events), workflow.quiesceCalls, len(store.stored), runtime.destroyed)
	}
}

func collectTerminalState(events iter.Seq2[a2atype.Event, error], result chan<- a2atype.TaskState) {
	for event, err := range events {
		if err != nil {
			result <- a2atype.TaskStateUnspecified
			return
		}
		if task, ok := event.(*a2atype.Task); ok && task.Status.State.Terminal() {
			result <- task.Status.State
			return
		}
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State.Terminal() {
			result <- update.Status.State
			return
		}
	}
}

func TestGatewayKeepsTaskActiveWhenQuiesceFails(t *testing.T) {
	store := &gatewayTestStore{instance: gatewayTestInstance()}
	workflow := &gatewayTestWorkflow{err: errors.New("snapshot failed")}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, &gatewayTestRuntime{})}, workflow, gatewayTestURL)

	sawError := false
	for event, err := range gateway.SendStreamingMessage(gatewayTestContext(), gatewayTestRequest()) {
		if task, ok := event.(*a2atype.Task); ok && task.Status.State == a2atype.TaskStateSubmitted && err == nil {
			continue
		}
		if event != nil || err == nil || sawError {
			t.Fatalf("stream result = %#v, %v", event, err)
		}
		sawError = true
	}
	if !sawError || workflow.quiesceCalls != 1 || len(store.stored) != 1 || store.active == nil {
		t.Fatalf("quiescence calls = %d, stored events = %d, active task = %#v", workflow.quiesceCalls, len(store.stored), store.active)
	}
}

func TestQuiescentTaskStates(t *testing.T) {
	for _, state := range []a2atype.TaskState{
		a2atype.TaskStateCompleted,
		a2atype.TaskStateCanceled,
		a2atype.TaskStateFailed,
		a2atype.TaskStateRejected,
		a2atype.TaskStateInputRequired,
		a2atype.TaskStateAuthRequired,
	} {
		if !isQuiescent(state) {
			t.Errorf("isQuiescent(%s) = false", state)
		}
	}
	if isQuiescent(a2atype.TaskStateWorking) {
		t.Error("working task is quiescent")
	}
}

func TestGatewayKeepsLiveRuntimeTaskBusy(t *testing.T) {
	active := &a2atype.Task{ID: "active", ContextID: gatewayTestID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
	runtime := &gatewayTestRuntime{task: active, subscribeEvent: active}
	store := &gatewayTestStore{instance: gatewayTestInstance(), active: active}
	dialer := &gatewayTestDialer{client: gatewayTestClient(t, runtime)}
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, &gatewayTestWorkflow{}, gatewayTestURL)

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
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)

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
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)

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
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)

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
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{err: errors.New("runtime unavailable")}, &gatewayTestWorkflow{}, gatewayTestURL)

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
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, &gatewayTestWorkflow{}, gatewayTestURL)

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
	gateway := New(store, &gatewayTestAuthorizer{}, dialer, &gatewayTestWorkflow{}, gatewayTestURL)

	if _, err := gateway.SendMessage(gatewayTestContext(), gatewayTestRequest()); err == nil {
		t.Fatal("SendMessage() accepted a reused message ID with different content")
	}
	if dialer.instance != nil {
		t.Fatal("conflicting message dialed the private runtime")
	}
}

// A denying authorizer, so "the share is what let this through" is provable rather
// than merely consistent with the result.
type gatewayDenyAuthorizer struct{ called bool }

func (a *gatewayDenyAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	a.called = true
	return errors.New("denied")
}

/*
 * Share links over an AgentInstance.
 *
 * The instance *is* the conversation, so sharing one is sharing what was said. Two
 * things have to hold, and neither is implied by the other:
 *
 *   - the share is authority over its own instance, and the record is read as the
 *     *owner* — an instance is scoped to its creator, so reading it as the visitor
 *     finds nothing and the link would 404;
 *   - the share is authority over nothing else, so a token for one instance cannot
 *     open another.
 */
func TestGatewayHonoursAgentInstanceShare(t *testing.T) {
	instance := gatewayTestInstance()
	store := &gatewayTestStore{instance: instance}
	authorizer := &gatewayDenyAuthorizer{}
	runtime := &gatewayTestRuntime{}
	gateway := New(store, authorizer, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, gatewayTestURL)

	ctx := auth.AuthSessionTo(t.Context(), gatewayTestSession{})
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{
		Token:           "share",
		UserID:          "the-owner",
		AgentInstanceID: instance.GetId(),
		ReadOnly:        true,
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		AgentInstanceNamespaceHeader, instance.GetNamespace(),
		AgentInstanceIDHeader, instance.GetId(),
	))

	if _, err := gateway.ListTasks(ctx, &a2atype.ListTasksRequest{}); err != nil {
		t.Fatalf("ListTasks() with a share for this instance = %v", err)
	}
	// Read as the owner: the visitor is somebody else, and an instance is scoped to
	// its creator.
	if store.userID != "the-owner" {
		t.Fatalf("instance read as %q, want the share's owner", store.userID)
	}
	if authorizer.called {
		t.Fatal("the ordinary authorization check should be skipped for a matching share")
	}
}

func TestGatewayRefusesAShareForADifferentInstance(t *testing.T) {
	instance := gatewayTestInstance()
	store := &gatewayTestStore{instance: instance}
	authorizer := &gatewayDenyAuthorizer{}
	gateway := New(store, authorizer, &gatewayTestDialer{}, &gatewayTestWorkflow{}, gatewayTestURL)

	ctx := auth.AuthSessionTo(t.Context(), gatewayTestSession{})
	// A perfectly valid share — for something else.
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{
		Token:           "share",
		UserID:          "the-owner",
		AgentInstanceID: "00000000-0000-0000-0000-000000000000",
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		AgentInstanceNamespaceHeader, instance.GetNamespace(),
		AgentInstanceIDHeader, instance.GetId(),
	))

	if _, err := gateway.ListTasks(ctx, &a2atype.ListTasksRequest{}); err == nil {
		t.Fatal("a share for another instance opened this one")
	}
	if !authorizer.called {
		t.Fatal("a non-matching share must fall through to the ordinary check")
	}
}

// A *session* share must not read as authority over an instance, however its id
// happens to be spelled. The two are separate fields for exactly this reason.
func TestGatewayIgnoresASessionShare(t *testing.T) {
	instance := gatewayTestInstance()
	authorizer := &gatewayDenyAuthorizer{}
	gateway := New(&gatewayTestStore{instance: instance}, authorizer, &gatewayTestDialer{}, &gatewayTestWorkflow{}, gatewayTestURL)

	ctx := auth.AuthSessionTo(t.Context(), gatewayTestSession{})
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{
		Token:     "share",
		UserID:    "the-owner",
		SessionID: instance.GetId(),
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		AgentInstanceNamespaceHeader, instance.GetNamespace(),
		AgentInstanceIDHeader, instance.GetId(),
	))

	if _, err := gateway.ListTasks(ctx, &a2atype.ListTasksRequest{}); err == nil {
		t.Fatal("a session share opened an AgentInstance")
	}
	if !authorizer.called {
		t.Fatal("a session share must fall through to the ordinary check")
	}
}

// TestGatewayRefusesButPreservesAParkedTurn is the reproduced defect and the
// decision about it. A turn that ends INPUT_REQUIRED holds the instance's single
// active-task slot, so every later send was refused as "already has an active
// task" — which reads as a broken agent. But that turn is a *valid pending
// question* (`ask_user` is a long-running call), so the send must be refused with
// a reason the reader can act on, and the question must survive: only the reader
// may give it up.
