// Package a2agateway exposes AgentInstances through the upstream A2A service.
// The initial handler establishes authenticated routing; the durable public
// Task and event layer will wrap runtime calls here rather than enter the gRPC
// transport or binary wiring.
package a2agateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"iter"
	"maps"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstancetask"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// TaskCreatedAtMetadataKey preserves the gateway's durable task creation time.
const TaskCreatedAtMetadataKey = "kagent.dev/task-created-at"

type instanceStore interface {
	GetAgentInstanceByID(context.Context, string) (*apiv1alpha1.AgentInstance, error)
	GetAgentInstance(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
	AdmitAgentInstanceTask(context.Context, string, []byte, *a2atype.SendMessageRequest, *a2atype.Task) (*database.TaskAdmission, error)
	AdmitAgentInstanceTaskContinuation(context.Context, string, []byte, *a2atype.SendMessageRequest) (*database.TaskAdmission, error)
	ClaimAgentInstanceTaskTurn(context.Context, string, string, uuid.UUID, uuid.UUID, time.Duration) (*database.TaskTurnClaim, bool, error)
	ReleaseAgentInstanceTaskTurn(context.Context, string, string, uuid.UUID, uuid.UUID) error
	MarkAgentInstanceTaskTurnIssued(context.Context, string, string, uuid.UUID, uuid.UUID) error
	RenewAgentInstanceTaskTurn(context.Context, string, string, uuid.UUID, uuid.UUID, time.Duration) (bool, error)
	RequestAgentInstanceTaskCancellation(context.Context, string, string) (*database.TaskCancellation, error)
	BeginAgentInstanceTaskCancellation(context.Context, string, string, uuid.UUID, uuid.UUID) (bool, error)
	BeginAgentInstanceTaskQuiescence(context.Context, string, string, uuid.UUID, uuid.UUID, a2atype.TaskState) error
	StoreOwnedAgentInstanceTaskEvent(context.Context, string, uuid.UUID, uuid.UUID, *a2atype.Task, a2atype.Event) error
	SettleAgentInstanceTaskTurn(context.Context, string, uuid.UUID, uuid.UUID, *a2atype.Task, a2atype.Event, *database.AgentInstanceTaskSnapshot) error
	GetAgentInstanceTask(context.Context, string, string, *int) (*a2atype.Task, error)
	GetAgentInstanceTaskObservation(context.Context, string, string, *int) (*database.TaskObservation, error)
	ListAgentInstanceTaskEvents(context.Context, string, string, int64, int) ([]database.TaskEvent, error)
	ListAgentInstanceTasks(context.Context, string, string, a2atype.TaskState, *time.Time, int, *int) ([]*a2atype.Task, int, error)
}

type runtimeDialer interface {
	Dial(context.Context, *apiv1alpha1.AgentInstance) (*a2aclient.Client, error)
}

type instanceWorkflow interface {
	Pause(context.Context, *apiv1alpha1.AgentInstance) error
	Quiesce(context.Context, *apiv1alpha1.AgentInstance) (*database.AgentInstanceTaskSnapshot, error)
}

// Gateway adapts upstream A2A requests. Durable task reads use API-owned services;
// execution remains in the gateway but requires database-backed turn ownership.
// The local run map only connects observers to an owner on this replica.
type Gateway struct {
	store       instanceStore
	tasks       *agentinstancetask.Service
	dialer      runtimeDialer
	workflow    instanceWorkflow
	gatewayURL  string
	runs        sync.Map
	turnLease   time.Duration
	turnRenewal time.Duration
	cancelPoll  time.Duration
}

var _ a2asrv.RequestHandler = (*Gateway)(nil)

// New returns the upstream A2A handler independently of any listener.
// Durable database ownership authorizes runtime effects; process-local coordination only
// serializes calls made by this replica.
func New(store instanceStore, authorizer auth.Authorizer, dialer runtimeDialer, workflow instanceWorkflow, gatewayURL string) a2asrv.RequestHandler {
	return &a2asrv.InterceptedHandler{
		Handler: &Gateway{store: store, tasks: agentinstancetask.NewService(store, authorizer), dialer: dialer, workflow: workflow,
			gatewayURL: gatewayURL,
			turnLease:  30 * time.Second, turnRenewal: 10 * time.Second, cancelPoll: 100 * time.Millisecond},
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

// storedInstance keeps transport routing in the gateway and delegates authorization
// and instance resolution to the API service.
func (g *Gateway) storedInstance(ctx context.Context, verb auth.Verb) (*apiv1alpha1.AgentInstance, error) {
	id, err := route(ctx)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, err.Error())
	}
	return g.tasks.Instance(ctx, id, verb)
}

func route(ctx context.Context) (string, error) {
	ids := metadata.ValueFromIncomingContext(ctx, apia2a.AgentInstanceIDHeader)
	if len(ids) != 1 {
		return "", fmt.Errorf("exactly one %s header is required", apia2a.AgentInstanceIDHeader)
	}
	id, err := uuid.Parse(ids[0])
	if err != nil {
		return "", fmt.Errorf("invalid %s header: %w", apia2a.AgentInstanceIDHeader, err)
	}
	return id.String(), nil
}

func (g *Gateway) GetTask(ctx context.Context, req *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	id, err := route(ctx)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, err.Error())
	}
	return g.tasks.GetTask(ctx, id, req)
}

func (g *Gateway) ListTasks(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	id, err := route(ctx)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, err.Error())
	}
	return g.tasks.ListTasks(ctx, id, req)
}

func (g *Gateway) CancelTask(ctx context.Context, req *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	instance, err := g.storedInstance(ctx, auth.VerbUpdate)
	if err != nil {
		return nil, err
	}
	if req == nil || req.ID == "" {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "task ID is required")
	}
	cancellation, err := g.store.RequestAgentInstanceTaskCancellation(ctx, instance.GetId(), string(req.ID))
	if errors.Is(err, database.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if errors.Is(err, database.ErrFailedPrecondition) {
		return nil, a2atype.ErrTaskNotCancelable
	}
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	if cancellation.Settled {
		return cancellation.Task, nil
	}
	if run, ok := g.taskRun(instance.GetId(), cancellation.Task.ID); ok && run.turnID == cancellation.TurnID && cancellation.OwnerID != nil && run.ownerID == *cancellation.OwnerID {
		run.signalCancel()
	}
	ticker := time.NewTicker(g.cancelPollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			task, err := g.store.GetAgentInstanceTask(ctx, instance.GetId(), string(req.ID), nil)
			if err != nil {
				return nil, g.storeError(ctx, err)
			}
			if task.Status.State.Terminal() {
				return task, nil
			}
		}
	}
}

func (g *Gateway) SendMessage(ctx context.Context, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	attempt, err := g.prepareSend(ctx, req)
	if err != nil {
		return nil, err
	}
	if !attempt.claimed {
		return attempt.task, nil
	}
	client, err := g.dialer.Dial(ctx, attempt.instance)
	if err != nil {
		g.releaseUnissued(ctx, attempt)
		logging.FromContext(ctx).ErrorContext(ctx, "failed to connect to agent instance runtime", "error", err, "instance_id", attempt.instance.GetId())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime")
	}
	run, err := g.startOwnedTaskRun(ctx, attempt, client)
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	result, err := client.SendMessage(context.WithoutCancel(ctx), attempt.request)
	if err != nil {
		run.fail(context.Background(), err)
		return attempt.task, err
	}
	if err := run.processResult(context.WithoutCancel(ctx), result); err != nil {
		run.fail(context.Background(), g.storeError(ctx, err))
		return nil, g.storeError(ctx, err)
	}
	task := run.currentTask()
	if !isQuiescent(task.Status.State) {
		run.startIngest(subscribeTask(run.ctx, client, &a2atype.SubscribeToTaskRequest{ID: task.ID}))
	}
	if _, ok := result.(*a2atype.Task); ok {
		return task, nil
	}
	return result, nil
}

func (g *Gateway) SubscribeToTask(ctx context.Context, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	id, err := route(ctx)
	if err != nil {
		return errorEvents(a2atype.NewError(a2atype.ErrInvalidRequest, err.Error()))
	}
	var updates agentinstancetask.TaskUpdates
	if req != nil {
		if run, ok := g.taskRun(id, req.ID); ok {
			updates = run
		}
	}
	return g.tasks.SubscribeToTask(ctx, id, req, updates)
}

func (g *Gateway) SendStreamingMessage(ctx context.Context, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	attempt, err := g.prepareSend(ctx, req)
	if err != nil {
		return errorEvents(err)
	}
	if !attempt.claimed {
		return func(yield func(a2atype.Event, error) bool) { yield(attempt.task, nil) }
	}
	client, err := g.dialer.Dial(ctx, attempt.instance)
	if err != nil {
		g.releaseUnissued(ctx, attempt)
		logging.FromContext(ctx).ErrorContext(ctx, "failed to connect to agent instance runtime", "error", err, "instance_id", attempt.instance.GetId())
		return errorEvents(a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime"))
	}
	run, err := g.startOwnedTaskRun(ctx, attempt, client)
	if err != nil {
		return errorEvents(g.storeError(ctx, err))
	}
	run.startIngest(client.SendStreamingMessage(run.ctx, attempt.request))
	return run.observe(ctx)
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
		logging.FromContext(ctx).ErrorContext(ctx, "failed to load agent instance runtime revision", "error", err, "revision", instance.GetPreparedRevision())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load Agent Card")
	}
	card, err := apia2a.FromProtoAgentCard(revision.AgentCard)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to decode agent instance agent card", "error", err, "revision", revision.Revision)
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
	request  *a2atype.SendMessageRequest
	turnID   uuid.UUID
	ownerID  uuid.UUID
	claimed  bool
}

func (g *Gateway) prepareSend(ctx context.Context, req *a2atype.SendMessageRequest) (*preparedSend, error) {
	verb := auth.VerbCreate
	if req != nil && req.Message != nil && req.Message.TaskID != "" {
		verb = auth.VerbUpdate
	}
	instance, err := g.storedInstance(ctx, verb)
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
	if req.Message.ContextID != "" && req.Message.ContextID != instance.GetContextId() {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message context does not match AgentInstance")
	}
	delete(req.Message.Metadata, apia2a.TimelinePositionMetadataKey)
	if req.Message.TaskID != "" {
		return g.prepareReply(ctx, instance, req)
	}
	requestHash, err := hashSendRequest(req)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message cannot be encoded")
	}
	req.Message.ContextID = instance.GetContextId()
	receivedAt := time.Now().UTC()
	req.Message.SetMeta(apia2a.TimelinePositionMetadataKey, receivedAt.Format(time.RFC3339Nano))
	req.Message.TaskID = a2atype.NewTaskID()
	submitted := a2atype.NewSubmittedTask(req.Message, req.Message)
	createdAt := receivedAt
	if submitted.Status.Timestamp != nil {
		createdAt = submitted.Status.Timestamp.UTC()
	}
	if submitted.Metadata == nil {
		submitted.Metadata = map[string]any{}
	}
	submitted.Metadata[TaskCreatedAtMetadataKey] = createdAt.Format(time.RFC3339Nano)
	admission, err := g.store.AdmitAgentInstanceTask(ctx, instance.GetId(), requestHash, req, submitted)
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	req.Message.TaskID = admission.Task.ID
	return g.claimAdmission(ctx, instance, admission)
}

func (g *Gateway) prepareReply(ctx context.Context, instance *apiv1alpha1.AgentInstance, req *a2atype.SendMessageRequest) (*preparedSend, error) {
	message := req.Message
	requestHash, err := hashSendRequest(req)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message cannot be encoded")
	}
	message.ContextID = instance.GetContextId()
	message.SetMeta(apia2a.TimelinePositionMetadataKey, time.Now().UTC().Format(time.RFC3339Nano))
	admission, err := g.store.AdmitAgentInstanceTaskContinuation(ctx, instance.GetId(), requestHash, req)
	if errors.Is(err, database.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	return g.claimAdmission(ctx, instance, admission)
}

func (g *Gateway) claimAdmission(ctx context.Context, instance *apiv1alpha1.AgentInstance, admission *database.TaskAdmission) (*preparedSend, error) {
	attempt := &preparedSend{instance: instance, task: admission.Task, turnID: admission.TurnID}
	if admission.TurnID == uuid.Nil {
		return attempt, nil
	}
	attempt.ownerID = uuid.New()
	claim, claimed, err := g.store.ClaimAgentInstanceTaskTurn(ctx, instance.GetId(), string(admission.Task.ID), admission.TurnID, attempt.ownerID, g.leaseDuration())
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	if !claimed {
		return attempt, nil
	}
	attempt.task, attempt.request, attempt.claimed = claim.Task, claim.Request, true
	if claim.Previous != nil {
		runtimeMessage := *attempt.request.Message
		runtimeMessage.Metadata = maps.Clone(attempt.request.Message.Metadata)
		if err := apia2a.AttachStoredTask(&runtimeMessage, claim.Previous); err != nil {
			_ = g.store.ReleaseAgentInstanceTaskTurn(ctx, instance.GetId(), string(admission.Task.ID), admission.TurnID, attempt.ownerID)
			return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to prepare task continuation")
		}
		attempt.request.Message = &runtimeMessage
	}
	return attempt, nil
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
		if result == nil {
			return nil, a2atype.NewError(a2atype.ErrInternalError, "runtime returned an empty task")
		}
		if createdAt, ok := submitted.Metadata[TaskCreatedAtMetadataKey]; ok {
			if result.Metadata == nil {
				result.Metadata = map[string]any{}
			}
			result.Metadata[TaskCreatedAtMetadataKey] = createdAt
		}
		return taskForEvent(submitted, result)
	case *a2atype.Message:
		if result == nil {
			return nil, a2atype.NewError(a2atype.ErrInternalError, "runtime returned an empty message")
		}
		return taskForEvent(submitted, result)
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

func requiresInput(state a2atype.TaskState) bool {
	return state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

func isQuiescent(state a2atype.TaskState) bool {
	return state.Terminal() || requiresInput(state)
}

func (g *Gateway) storeError(ctx context.Context, err error) error {
	if errors.Is(err, database.ErrFailedPrecondition) {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "reply does not match the pending input request")
	}
	if errors.Is(err, database.ErrIdempotencyConflict) {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "message ID was already used with a different request")
	}
	if errors.Is(err, database.ErrConflict) {
		return a2atype.NewError(a2atype.ErrUnsupportedOperation, err.Error())
	}
	logging.FromContext(ctx).ErrorContext(ctx, "failed to persist agent instance task", "error", err)
	return a2atype.NewError(a2atype.ErrInternalError, "failed to persist task")
}

func errorEvents(err error) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		var zero a2atype.Event
		yield(zero, err)
	}
}

func (g *Gateway) releaseUnissued(ctx context.Context, attempt *preparedSend) {
	if err := g.store.ReleaseAgentInstanceTaskTurn(context.WithoutCancel(ctx), attempt.instance.GetId(), string(attempt.task.ID), attempt.turnID, attempt.ownerID); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to release unissued task turn", "error", err,
			"instance_id", attempt.instance.GetId(), "task_id", attempt.task.ID, "turn_id", attempt.turnID)
	}
}

func (g *Gateway) leaseDuration() time.Duration {
	if g.turnLease > 0 {
		return g.turnLease
	}
	return 30 * time.Second
}

func (g *Gateway) renewalInterval() time.Duration {
	if g.turnRenewal > 0 {
		return g.turnRenewal
	}
	return g.leaseDuration() / 3
}

func (g *Gateway) cancelPollInterval() time.Duration {
	if g.cancelPoll > 0 {
		return g.cancelPoll
	}
	return 100 * time.Millisecond
}
