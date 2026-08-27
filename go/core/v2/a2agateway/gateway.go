// Package a2agateway exposes AgentInstances through the upstream A2A service.
// The initial handler establishes authenticated routing; the durable public
// Task and event layer will wrap runtime calls here rather than enter the gRPC
// transport or binary wiring.
package a2agateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"strings"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// AgentInstanceNamespaceHeader selects the Kubernetes namespace containing the AgentInstance.
	AgentInstanceNamespaceHeader = "x-kagent-agent-instance-namespace"
	// AgentInstanceIDHeader selects the AgentInstance within that namespace.
	AgentInstanceIDHeader = "x-kagent-agent-instance-id"
	// TaskCreatedAtMetadataKey preserves the gateway's durable task creation time.
	TaskCreatedAtMetadataKey = "kagent.dev/task-created-at"
)

type instanceStore interface {
	GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error)
	GetRuntimeRevision(context.Context, string) (*dbpkg.RuntimeRevision, error)
	CreateAgentInstanceTask(context.Context, string, []byte, *a2atype.Task) (*a2atype.Task, bool, error)
	GetActiveAgentInstanceTask(context.Context, string) (*a2atype.Task, error)
	InterruptActiveAgentInstanceTask(context.Context, string, string) (bool, error)
	StoreAgentInstanceTaskEvent(context.Context, string, *a2atype.Task, a2atype.Event, *dbpkg.AgentInstanceTaskSnapshot) error
	GetAgentInstanceTask(context.Context, string, string) (*a2atype.Task, error)
	ListAgentInstanceTasks(context.Context, string, string, a2atype.TaskState, *time.Time, int) ([]*a2atype.Task, int, error)
}

type runtimeDialer interface {
	Dial(context.Context, *apiv1alpha1.AgentInstance) (*a2aclient.Client, error)
}

type instanceWorkflow interface {
	Quiesce(context.Context, *apiv1alpha1.AgentInstance) (*dbpkg.AgentInstanceTaskSnapshot, error)
}

type runtimeCoordinator interface {
	RuntimeCall(string) func()
	Quiesce(string) func()
}

type memoryRuntimeCoordinator struct {
	locks sync.Map
}

var processRuntimeCoordinator = &memoryRuntimeCoordinator{}

func (c *memoryRuntimeCoordinator) lock(instanceID string) *sync.RWMutex {
	lock, _ := c.locks.LoadOrStore(instanceID, &sync.RWMutex{})
	return lock.(*sync.RWMutex)
}

func (c *memoryRuntimeCoordinator) RuntimeCall(instanceID string) func() {
	lock := c.lock(instanceID)
	lock.RLock()
	return lock.RUnlock
}

func (c *memoryRuntimeCoordinator) Quiesce(instanceID string) func() {
	lock := c.lock(instanceID)
	lock.Lock()
	return lock.Unlock
}

// Gateway is transport-neutral. The v0 deployment registers it on the
// controller's gRPC server, while a standalone gateway can register the same
// handler on its own server later.
type Gateway struct {
	store       instanceStore
	authorizer  auth.Authorizer
	dialer      runtimeDialer
	workflow    instanceWorkflow
	gatewayURL  string
	events      eventqueue.Manager
	runs        sync.Map
	coordinator runtimeCoordinator
}

var _ a2asrv.RequestHandler = (*Gateway)(nil)

// New returns the upstream A2A handler independently of any listener or gRPC
// server, keeping deployment topology outside the gateway package.
//
// ponytail: coordination is process-local. Gateway deployments must remain at
// one replica until this is replaced by a PostgreSQL-backed coordinator.
func New(store instanceStore, authorizer auth.Authorizer, dialer runtimeDialer, workflow instanceWorkflow, gatewayURL string) a2asrv.RequestHandler {
	return newGateway(store, authorizer, dialer, workflow, gatewayURL, processRuntimeCoordinator)
}

func newGateway(store instanceStore, authorizer auth.Authorizer, dialer runtimeDialer, workflow instanceWorkflow, gatewayURL string, coordinator runtimeCoordinator) a2asrv.RequestHandler {
	return &a2asrv.InterceptedHandler{
		Handler: &Gateway{
			store: store, authorizer: authorizer, dialer: dialer, workflow: workflow,
			gatewayURL: gatewayURL, events: eventqueue.NewInMemoryManager(), coordinator: coordinator,
		},
		Interceptors: []a2asrv.CallInterceptor{a2aext.NewServerPropagator(nil)},
	}
}

func (g *Gateway) instance(ctx context.Context, verb auth.Verb) (*apiv1alpha1.AgentInstance, error) {
	instance, err := g.storedInstance(ctx, verb)
	if err != nil {
		return nil, err
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, fmt.Sprintf("AgentInstance is %s", instance.GetState()))
	}
	return instance, nil
}

/*
 * Resolves the routed instance, whatever state it is in.
 *
 * Authorization is unchanged — an instance is still read as its creator, and a share
 * token still only widens reach to the instance it names. What is dropped is the
 * readiness requirement, because it was never this function's to impose: a task list
 * and a task come out of the store, and the store does not care whether the instance
 * currently holds a worker.
 *
 * Requiring READY for those reads made a suspended conversation unreadable, which is a
 * real problem now that conversations give their workers back at the end of every turn:
 * opening one to re-read what was said reported "AgentInstance is
 * AGENT_INSTANCE_STATE_SUSPENDED" as if the record had been lost. The alternative —
 * resuming on open — would claim a worker every time somebody glanced at a transcript,
 * which is exactly what suspending them was meant to stop.
 */
func (g *Gateway) storedInstance(ctx context.Context, verb auth.Verb) (*apiv1alpha1.AgentInstance, error) {
	namespace, id, err := route(ctx)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, err.Error())
	}
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, a2atype.NewError(a2atype.ErrUnauthenticated, "authentication is required")
	}
	principal := session.Principal()

	/*
	 * A share token is authority over one instance, and only that one.
	 *
	 * The visitor is still authenticated as themselves — a share widens what an
	 * account may reach, it does not replace authentication — so the ordinary
	 * authorization check is skipped only when the token names *this* instance, and
	 * the record is then read as its owner. Reading it as the visitor would find
	 * nothing, because an instance is scoped to its creator.
	 *
	 * The read-only half is enforced in the interceptor, which refuses a
	 * write-access RPC for a read-only share before this is reached.
	 */
	creator := principal.User.ID
	share, hasShare := auth.ShareContextFrom(ctx)
	if hasShare && share.IsForAgentInstance(id) {
		creator = share.UserID
	} else if err := g.authorizer.Check(ctx, principal, verb, auth.Resource{Type: "AgentInstance", Name: namespace + "/" + id}); err != nil {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	instance, err := g.store.GetAgentInstance(ctx, namespace, id, creator)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to load AgentInstance", "namespace", namespace, "id", id)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load AgentInstance")
	}
	return instance, nil
}

func route(ctx context.Context) (namespace, id string, err error) {
	namespaces := metadata.ValueFromIncomingContext(ctx, AgentInstanceNamespaceHeader)
	ids := metadata.ValueFromIncomingContext(ctx, AgentInstanceIDHeader)
	if len(namespaces) != 1 || len(ids) != 1 {
		return "", "", fmt.Errorf("exactly one %s and %s header is required", AgentInstanceNamespaceHeader, AgentInstanceIDHeader)
	}
	if problems := utilvalidation.IsDNS1123Label(namespaces[0]); len(problems) > 0 {
		return "", "", fmt.Errorf("invalid %s header: %s", AgentInstanceNamespaceHeader, strings.Join(problems, "; "))
	}
	parsedID, err := uuid.Parse(ids[0])
	if err != nil {
		return "", "", fmt.Errorf("invalid %s header: %w", AgentInstanceIDHeader, err)
	}
	return namespaces[0], parsedID.String(), nil
}

func (g *Gateway) GetTask(ctx context.Context, req *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	instance, err := g.storedInstance(ctx, auth.VerbGet)
	if err != nil {
		return nil, err
	}
	if req == nil || req.ID == "" {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "task ID is required")
	}
	task, err := g.store.GetAgentInstanceTask(ctx, instance.GetId(), string(req.ID))
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to load AgentInstance task", "task", req.ID)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load task")
	}
	return shapeTask(task, req.HistoryLength, true), nil
}

func (g *Gateway) ListTasks(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	instance, err := g.storedInstance(ctx, auth.VerbGet)
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &a2atype.ListTasksRequest{}
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "page size must be between 1 and 100")
	}
	if req.ContextID != "" && req.ContextID != instance.GetId() {
		return &a2atype.ListTasksResponse{Tasks: []*a2atype.Task{}, PageSize: pageSize}, nil
	}
	afterID, err := decodePageToken(req.PageToken)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "invalid page token")
	}
	tasks, total, err := g.store.ListAgentInstanceTasks(ctx, instance.GetId(), afterID, req.Status, req.StatusTimestampAfter, pageSize+1)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list AgentInstance tasks", "instance", instance.GetId())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to list tasks")
	}
	response := &a2atype.ListTasksResponse{Tasks: tasks, TotalSize: total, PageSize: pageSize}
	if len(tasks) > pageSize {
		response.Tasks = tasks[:pageSize]
		response.NextPageToken = encodePageToken(string(response.Tasks[pageSize-1].ID))
	}
	for i, task := range response.Tasks {
		response.Tasks[i] = shapeTask(task, req.HistoryLength, req.IncludeArtifacts)
	}
	return response, nil
}

func (g *Gateway) CancelTask(ctx context.Context, req *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	instance, err := g.instance(ctx, auth.VerbUpdate)
	if err != nil {
		return nil, err
	}
	if req == nil || req.ID == "" {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "task ID is required")
	}
	task, err := g.store.GetAgentInstanceTask(ctx, instance.GetId(), string(req.ID))
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to load AgentInstance task", "task", req.ID)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load task")
	}
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to connect to AgentInstance runtime", "instance", instance.GetId())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime")
	}
	release := g.coordinator.RuntimeCall(instance.GetId())
	defer release()
	defer client.Destroy()
	canceled, err := client.CancelTask(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := validateTaskInfo(canceled, task); err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, err.Error())
	}
	return canceled, nil
}

func (g *Gateway) SendMessage(ctx context.Context, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	attempt, err := g.prepareSend(ctx, req)
	if err != nil {
		return nil, err
	}
	if !attempt.dispatch {
		return attempt.task, nil
	}
	client, err := g.dialer.Dial(ctx, attempt.instance)
	if err != nil {
		g.failAttempt(ctx, attempt)
		ctrllog.FromContext(ctx).Error(err, "failed to connect to AgentInstance runtime", "instance", attempt.instance.GetId())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime")
	}
	defer client.Destroy()
	result, err := client.SendMessage(ctx, req)
	if err != nil {
		g.failAttempt(ctx, attempt)
		return nil, err
	}
	task, err := taskForResult(attempt.task, result)
	if err != nil {
		g.failAttempt(ctx, attempt)
		return nil, err
	}
	if err := g.storeEvent(ctx, attempt.instance, task, result); err != nil {
		return nil, g.storeError(ctx, err)
	}
	return result, nil
}

func (g *Gateway) SubscribeToTask(ctx context.Context, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	instance, err := g.instance(ctx, auth.VerbGet)
	if err != nil {
		return errorEvents(err)
	}
	if req == nil || req.ID == "" {
		return errorEvents(a2atype.NewError(a2atype.ErrInvalidRequest, "task ID is required"))
	}
	task, err := g.store.GetAgentInstanceTask(ctx, instance.GetId(), string(req.ID))
	if errors.Is(err, dbpkg.ErrNotFound) {
		return errorEvents(a2atype.ErrTaskNotFound)
	}
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to load AgentInstance task", "task", req.ID)
		return errorEvents(a2atype.NewError(a2atype.ErrInternalError, "failed to load task"))
	}
	if isQuiescent(task.Status.State) {
		return func(yield func(a2atype.Event, error) bool) { yield(task, nil) }
	}
	if run, ok := g.taskRun(instance.GetId(), task.ID); ok {
		return run.observe(ctx, task)
	}
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to connect to AgentInstance runtime", "instance", instance.GetId())
		return errorEvents(a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime"))
	}
	run, reader, err := g.startTaskRun(ctx, instance, task, nil, client, client.SubscribeToTask(context.WithoutCancel(ctx), req))
	if err != nil {
		_ = client.Destroy()
		if run, ok := g.taskRun(instance.GetId(), task.ID); ok {
			return run.observe(ctx, task)
		}
		return errorEvents(g.storeError(ctx, err))
	}
	return run.observeReader(ctx, task, reader)
}

func (g *Gateway) SendStreamingMessage(ctx context.Context, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	attempt, err := g.prepareSend(ctx, req)
	if err != nil {
		return errorEvents(err)
	}
	if !attempt.dispatch {
		return func(yield func(a2atype.Event, error) bool) { yield(attempt.task, nil) }
	}
	client, err := g.dialer.Dial(ctx, attempt.instance)
	if err != nil {
		g.failAttempt(ctx, attempt)
		ctrllog.FromContext(ctx).Error(err, "failed to connect to AgentInstance runtime", "instance", attempt.instance.GetId())
		return errorEvents(a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime"))
	}
	run, reader, err := g.startTaskRun(ctx, attempt.instance, attempt.task, attempt.previous, client, client.SendStreamingMessage(context.WithoutCancel(ctx), req))
	if err != nil {
		_ = client.Destroy()
		g.failAttempt(ctx, attempt)
		return errorEvents(g.storeError(ctx, err))
	}
	return run.observeReader(ctx, attempt.task, reader)
}

func (g *Gateway) GetTaskPushConfig(ctx context.Context, req *a2atype.GetTaskPushConfigRequest) (*a2atype.PushConfig, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) ListTaskPushConfigs(ctx context.Context, req *a2atype.ListTaskPushConfigRequest) (*a2atype.ListTaskPushConfigResponse, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) CreateTaskPushConfig(ctx context.Context, req *a2atype.PushConfig) (*a2atype.PushConfig, error) {
	return nil, a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) DeleteTaskPushConfig(ctx context.Context, req *a2atype.DeleteTaskPushConfigRequest) error {
	return a2atype.ErrPushNotificationNotSupported
}

func (g *Gateway) GetExtendedAgentCard(ctx context.Context, _ *a2atype.GetExtendedAgentCardRequest) (*a2atype.AgentCard, error) {
	instance, err := g.instance(ctx, auth.VerbGet)
	if err != nil {
		return nil, err
	}
	revision, err := g.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to load AgentInstance runtime revision", "revision", instance.GetPreparedRevision())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load Agent Card")
	}
	card := &a2atype.AgentCard{}
	if err := json.Unmarshal(revision.AgentCard, card); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to decode AgentInstance Agent Card", "revision", revision.Revision)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load Agent Card")
	}

	// The compiled card provides immutable template metadata. Public transport,
	// security, and signatures belong to the gateway instead of the private
	// runtime that produced that card.
	card.SupportedInterfaces = []*a2atype.AgentInterface{a2atype.NewAgentInterface(g.gatewayURL, a2atype.TransportProtocolGRPC)}
	// Extensions are the exception, and replacing the whole capabilities struct
	// used to drop them. They describe what the runtime behind this gateway can
	// negotiate — human-in-the-loop among them — which is not the gateway's to
	// erase. A client discovers HITL by reading this card, so wiping it made
	// answering an agent's question undiscoverable while the card still rendered
	// perfectly.
	extensions := card.Capabilities.Extensions
	card.Capabilities = a2atype.AgentCapabilities{Streaming: true, ExtendedAgentCard: true, Extensions: extensions}
	card.SecurityRequirements = nil
	card.SecuritySchemes = nil
	card.Signatures = nil
	return card, nil
}

type preparedSend struct {
	instance *apiv1alpha1.AgentInstance
	task     *a2atype.Task
	previous *a2atype.Task
	dispatch bool
}

func (g *Gateway) prepareSend(ctx context.Context, req *a2atype.SendMessageRequest) (*preparedSend, error) {
	verb := auth.VerbCreate
	if req != nil && req.Message != nil && req.Message.TaskID != "" {
		verb = auth.VerbUpdate
	}
	instance, err := g.instance(ctx, verb)
	if err != nil {
		return nil, err
	}
	if req == nil || req.Message == nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message is required")
	}
	apia2a.ClearStoredTask(req.Message)
	if req.Message.ID == "" {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message ID is required")
	}
	if req.Message.ContextID != "" && req.Message.ContextID != instance.GetId() {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message context does not match AgentInstance")
	}
	if req.Message.TaskID != "" {
		return g.prepareReply(ctx, instance, req)
	}
	req.Message.ContextID = instance.GetId()
	requestHash, err := hashSendRequest(req)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message cannot be encoded")
	}
	req.Message.TaskID = a2atype.NewTaskID()
	submitted := a2atype.NewSubmittedTask(req.Message, req.Message)
	createdAt := time.Now().UTC()
	if submitted.Status.Timestamp != nil {
		createdAt = submitted.Status.Timestamp.UTC()
	}
	if submitted.Metadata == nil {
		submitted.Metadata = map[string]any{}
	}
	submitted.Metadata[TaskCreatedAtMetadataKey] = createdAt.Format(time.RFC3339Nano)
	stored, created, err := g.store.CreateAgentInstanceTask(ctx, instance.GetId(), requestHash, submitted)
	if errors.Is(err, dbpkg.ErrAgentInstanceTaskConflict) {
		if err = g.reconcileActiveTask(ctx, instance); err == nil {
			stored, created, err = g.store.CreateAgentInstanceTask(ctx, instance.GetId(), requestHash, submitted)
		}
	}
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	req.Message.TaskID = stored.ID
	return &preparedSend{instance: instance, task: stored, dispatch: created}, nil
}

func (g *Gateway) prepareReply(ctx context.Context, instance *apiv1alpha1.AgentInstance, req *a2atype.SendMessageRequest) (*preparedSend, error) {
	message := req.Message
	stored, err := g.store.GetAgentInstanceTask(ctx, instance.GetId(), string(message.TaskID))
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	if stored.ContextID != instance.GetId() {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "task context does not match AgentInstance")
	}
	if stored.Status.State != a2atype.TaskStateInputRequired && stored.Status.State != a2atype.TaskStateAuthRequired {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "task is not waiting for input")
	}
	message.ContextID = stored.ContextID
	attempt := *stored
	attempt.History = append(append([]*a2atype.Message{}, stored.History...), message)
	now := time.Now()
	attempt.Status = a2atype.TaskStatus{State: a2atype.TaskStateSubmitted, Timestamp: &now}
	if err := g.store.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), &attempt, message, nil); err != nil {
		return nil, g.storeError(ctx, err)
	}
	runtimeMessage := *message
	runtimeMessage.Metadata = maps.Clone(message.Metadata)
	if err := apia2a.AttachStoredTask(&runtimeMessage, stored); err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to prepare task continuation")
	}
	req.Message = &runtimeMessage
	return &preparedSend{instance: instance, task: &attempt, previous: stored, dispatch: true}, nil
}

// reconcileActiveTask frees the task slot only when the runtime authoritatively
// reports that the exact active task has no execution, or reports it terminal.
func (g *Gateway) reconcileActiveTask(ctx context.Context, instance *apiv1alpha1.AgentInstance) error {
	active, err := g.store.GetActiveAgentInstanceTask(ctx, instance.GetId())
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to reconcile active AgentInstance task", "task", active.ID)
		return dbpkg.ErrAgentInstanceTaskConflict
	}
	defer client.Destroy()

	// An active execution immediately yields its current event. TaskNotFound
	// means no execution remains, so only the first result is needed.
	for event, eventErr := range client.SubscribeToTask(ctx, &a2atype.SubscribeToTaskRequest{ID: active.ID}) {
		if errors.Is(eventErr, a2atype.ErrTaskNotFound) {
			latest, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: active.ID})
			if err != nil || latest == nil {
				return dbpkg.ErrAgentInstanceTaskConflict
			}
			if err := validateTaskInfo(latest, active); err != nil {
				ctrllog.FromContext(ctx).Error(err, "runtime returned invalid active task", "task", active.ID)
				return dbpkg.ErrAgentInstanceTaskConflict
			}
			if isQuiescent(latest.Status.State) {
				return g.storeEvent(ctx, instance, latest, latest)
			}
			return g.interruptTask(ctx, instance.GetId(), active.ID)
		}
		if eventErr != nil {
			ctrllog.FromContext(ctx).Error(eventErr, "failed to query active runtime execution", "task", active.ID)
			return dbpkg.ErrAgentInstanceTaskConflict
		}
		if event == nil {
			return dbpkg.ErrAgentInstanceTaskConflict
		}
		if err := validateTaskInfo(event, active); err != nil {
			ctrllog.FromContext(ctx).Error(err, "runtime returned invalid active task event", "task", active.ID)
			return dbpkg.ErrAgentInstanceTaskConflict
		}
		return dbpkg.ErrAgentInstanceTaskConflict
	}
	return dbpkg.ErrAgentInstanceTaskConflict
}

func (g *Gateway) interruptTask(ctx context.Context, instanceID string, taskID a2atype.TaskID) error {
	interrupted, err := g.store.InterruptActiveAgentInstanceTask(ctx, instanceID, string(taskID))
	if err != nil {
		return err
	}
	if !interrupted {
		return dbpkg.ErrAgentInstanceTaskConflict
	}
	return nil
}

func hashSendRequest(req *a2atype.SendMessageRequest) ([]byte, error) {
	pb, err := pbconv.ToProtoSendMessageRequest(req)
	if err != nil {
		return nil, err
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(pb)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return sum[:], nil
}

func taskForResult(submitted *a2atype.Task, result a2atype.SendMessageResult) (*a2atype.Task, error) {
	switch result := result.(type) {
	case *a2atype.Task:
		if err := validateTaskInfo(result, submitted); err != nil {
			return nil, a2atype.NewError(a2atype.ErrInternalError, err.Error())
		}
		if createdAt, ok := submitted.Metadata[TaskCreatedAtMetadataKey]; ok {
			if result.Metadata == nil {
				result.Metadata = map[string]any{}
			}
			result.Metadata[TaskCreatedAtMetadataKey] = createdAt
		}
		return result, nil
	case *a2atype.Message:
		if result.TaskID == "" {
			result.TaskID = submitted.ID
		}
		if result.ContextID == "" {
			result.ContextID = submitted.ContextID
		}
		if err := validateTaskInfo(result, submitted); err != nil {
			return nil, a2atype.NewError(a2atype.ErrInternalError, err.Error())
		}
		task := *submitted
		task.History = append(append([]*a2atype.Message{}, submitted.History...), result)
		now := time.Now()
		task.Status = a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &now}
		return &task, nil
	default:
		return nil, a2atype.NewError(a2atype.ErrInternalError, fmt.Sprintf("runtime returned unsupported result %T", result))
	}
}

func taskForEvent(task *a2atype.Task, event a2atype.Event) (*a2atype.Task, error) {
	if event == nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, "runtime returned an empty event")
	}
	if message, ok := event.(*a2atype.Message); ok {
		if message.TaskID == "" {
			message.TaskID = task.ID
		}
		if message.ContextID == "" {
			message.ContextID = task.ContextID
		}
	}
	if err := validateTaskInfo(event, task); err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, err.Error())
	}
	if message, ok := event.(*a2atype.Message); ok {
		copy := *task
		copy.History = append(append([]*a2atype.Message{}, task.History...), message)
		now := time.Now()
		copy.Status = a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &now}
		return &copy, nil
	}
	updated, err := a2aevent.ApplyUpdate(task, event)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, fmt.Sprintf("apply runtime task event: %v", err))
	}
	/*
	 * The runtime may send a whole task, and it does not always remember as much as
	 * the store does.
	 *
	 * `ApplyUpdate` takes the runtime's version where one is given, which is right for
	 * status and artifacts and wrong for history: a runtime that has been quiesced and
	 * resumed can answer with a task carrying no history at all, and persisting that
	 * replaces a transcript with an empty one. That is not a display problem — the
	 * messages are gone from the record, and the conversation opens blank.
	 *
	 * Seen doing exactly that: a conversation parked on a question, answered after the
	 * runtime had been suspended, came back as an eighty-byte task while its six events
	 * sat untouched in the store beside it.
	 *
	 * So history only ever grows here. A runtime that genuinely has more is believed;
	 * one that has less is not allowed to forget on the store's behalf.
	 */
	if len(updated.History) < len(task.History) {
		kept := *updated
		kept.History = task.History
		return &kept, nil
	}
	return updated, nil
}

func validateTaskInfo(value a2atype.TaskInfoProvider, expected *a2atype.Task) error {
	info := value.TaskInfo()
	if info.TaskID != expected.ID || info.ContextID != expected.ContextID {
		return fmt.Errorf("runtime returned mismatched task identity")
	}
	return nil
}

func (g *Gateway) storeEvent(ctx context.Context, instance *apiv1alpha1.AgentInstance, task *a2atype.Task, event a2atype.Event) error {
	var snapshot *dbpkg.AgentInstanceTaskSnapshot
	if task != nil && isQuiescent(task.Status.State) {
		var err error
		snapshot, err = g.workflow.Quiesce(ctx, instance)
		if err != nil {
			return fmt.Errorf("quiesce AgentInstance runtime: %w", err)
		}
	}
	return g.store.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), task, event, snapshot)
}

func isQuiescent(state a2atype.TaskState) bool {
	return state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

func (g *Gateway) failTask(ctx context.Context, instanceID string, task *a2atype.Task) {
	now := time.Now()
	failed := *task
	failed.Status = a2atype.TaskStatus{State: a2atype.TaskStateFailed, Timestamp: &now}
	if err := g.store.StoreAgentInstanceTaskEvent(ctx, instanceID, &failed, &failed, nil); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to record failed AgentInstance task", "task", task.ID)
	}
}

func (g *Gateway) failAttempt(ctx context.Context, attempt *preparedSend) {
	if attempt.previous == nil {
		g.failTask(ctx, attempt.instance.GetId(), attempt.task)
		return
	}
	if err := g.store.StoreAgentInstanceTaskEvent(ctx, attempt.instance.GetId(), attempt.previous, attempt.previous, nil); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to restore task awaiting input", "task", attempt.task.ID)
	}
}

func (g *Gateway) storeError(ctx context.Context, err error) error {
	if errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "message ID was already used with a different request")
	}
	if errors.Is(err, dbpkg.ErrAgentInstanceTaskConflict) {
		return a2atype.NewError(a2atype.ErrUnsupportedOperation, "AgentInstance already has an active task")
	}
	ctrllog.FromContext(ctx).Error(err, "failed to persist AgentInstance task")
	return a2atype.NewError(a2atype.ErrInternalError, "failed to persist task")
}

func shapeTask(task *a2atype.Task, historyLength *int, includeArtifacts bool) *a2atype.Task {
	result := *task
	if historyLength != nil {
		switch {
		case *historyLength == 0:
			result.History = []*a2atype.Message{}
		case *historyLength > 0 && *historyLength < len(result.History):
			result.History = result.History[len(result.History)-*historyLength:]
		}
	}
	if !includeArtifacts {
		result.Artifacts = nil
	}
	return &result
}

func encodePageToken(taskID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(taskID))
}

func decodePageToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return "", fmt.Errorf("invalid page token")
	}
	return string(decoded), nil
}

func errorEvents(err error) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		var zero a2atype.Event
		yield(zero, err)
	}
}
