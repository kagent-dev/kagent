// Package a2agateway exposes Agents and their conversations through upstream A2A.
// It serves public stored history and proxies authorized interactions to the
// runtime, which owns execution and persistence independently of observers.
package a2agateway

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstance"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"k8s.io/apimachinery/pkg/types"
)

type instanceStore interface {
	ReserveAgentInstanceDispatch(context.Context, string, uuid.UUID, string) error
	RevokeAgentInstanceDispatch(context.Context, string, uuid.UUID, string) (bool, error)
	GetAgentInstanceByID(context.Context, string) (*apiv1alpha1.AgentInstance, error)
	GetAgentInstance(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
	GetAgentInstanceTaskByMessage(context.Context, string, string, string) (*a2atype.Task, error)
	GetAgentInstanceTask(context.Context, string, string, *int) (*a2atype.Task, error)
	GetSettledAgentInstanceTask(context.Context, string, string, *int) (*a2atype.Task, error)
	ListAgentTasks(context.Context, []string, string, a2atype.TaskState, *time.Time, int, *int) ([]*a2atype.Task, int, error)
	AgentInstanceForTask(context.Context, string) (string, error)
}

type agentService interface {
	Get(context.Context, types.NamespacedName) (*v1alpha3.Agent, error)
}

type instanceService interface {
	Create(context.Context, *apiv1alpha1.ResourceReference, string, string) (*apiv1alpha1.AgentInstance, error)
	List(context.Context, agentinstance.ListRequest) (agentinstance.ListResult, error)
}

type Config struct {
	Store      instanceStore
	Authorizer auth.Authorizer
	Dialer     runtimeDialer
	Agents     agentService
	Instances  instanceService
	GatewayURL string
}

type runtimeDialer interface {
	Dial(context.Context, *apiv1alpha1.AgentInstance) (*a2aclient.Client, error)
}

// Gateway authorizes public A2A requests and routes them to the runtime. Reads
// come from the central store; observers never become task writers or executors.
type Gateway struct {
	store      instanceStore
	authorizer auth.Authorizer
	dialer     runtimeDialer
	gatewayURL string
	agents     agentService
	instances  instanceService
}

var _ a2asrv.RequestHandler = (*Gateway)(nil)

// runtimeDrainTimeout bounds the wait for a terminal stream to finish exporting
// its request telemetry before the observer closes the runtime connection.
var runtimeDrainTimeout = 2 * time.Second

func New(config Config) a2asrv.RequestHandler {
	return &a2asrv.InterceptedHandler{
		Handler:      &Gateway{store: config.Store, authorizer: config.Authorizer, dialer: config.Dialer, gatewayURL: config.GatewayURL, agents: config.Agents, instances: config.Instances},
		Interceptors: []a2asrv.CallInterceptor{a2aext.NewServerPropagator(nil)},
	}
}

/*
 * Resolves the routed instance, whatever state it is in.
 *
 * Human callers read an instance as its creator, and a share
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
func (g *Gateway) storedInstance(ctx context.Context, verb auth.Verb, agent *apiv1alpha1.ResourceReference, contextID string) (*apiv1alpha1.AgentInstance, error) {
	parsed, err := uuid.Parse(contextID)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "invalid context ID")
	}
	id := parsed.String()
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
	 * Share permissions are enforced here for every A2A transport. Transport
	 * middleware only authenticates the caller and resolves the share token.
	 */
	creator := principal.User.ID
	share, hasShare := auth.ShareContextFrom(ctx)
	if hasShare && share.ReadOnly && verb != auth.VerbGet {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "this share link is read-only")
	}
	if hasShare && share.IsForAgentInstance(id) {
		creator = share.UserID
	} else if err := g.authorizer.Check(ctx, principal, verb, auth.Resource{Type: "AgentInstance", Name: id}); err != nil {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	var instance *apiv1alpha1.AgentInstance
	// Only the authenticated internal session can read independently of ownership.
	// Authorization above still evaluates the actual control-plane principal.
	if _, controlPlane := session.(auth.ControlPlaneSession); controlPlane && !hasShare {
		instance, err = g.store.GetAgentInstanceByID(ctx, id)
	} else {
		instance, err = g.store.GetAgentInstance(ctx, id, creator)
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to load agent instance", "error", err, "instance_id", id)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load AgentInstance")
	}
	if instance.GetAgent().GetNamespace() != agent.Namespace || instance.GetAgent().GetName() != agent.Name {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "context does not belong to this Agent")
	}
	return instance, nil
}

func (g *Gateway) GetTask(ctx context.Context, req *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	if req == nil {
		return nil, a2atype.ErrInvalidParams
	}
	instance, err := g.taskInstance(ctx, auth.VerbGet, req.Tenant, req.ID)
	if err != nil {
		return nil, err
	}
	task, err := g.store.GetAgentInstanceTask(ctx, instance.GetId(), string(req.ID), req.HistoryLength)
	if errors.Is(err, database.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to load agent instance task", "error", err, "task_id", req.ID)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load task")
	}
	return shapeTask(task, req.HistoryLength, true), nil
}

func (g *Gateway) ListTasks(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	if req == nil {
		req = &a2atype.ListTasksRequest{}
	}
	agent, err := route(ctx, req.Tenant)
	if err != nil {
		return nil, err
	}
	instanceIDs, err := g.listInstances(ctx, agent, req.ContextID)
	if err != nil {
		return nil, err
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "page size must be between 1 and 100")
	}

	afterID, err := decodePageToken(req.PageToken)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "invalid page token")
	}
	tasks, total, err := g.store.ListAgentTasks(ctx, instanceIDs, afterID, req.Status, req.StatusTimestampAfter, pageSize+1, req.HistoryLength)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list agent instance tasks", "error", err)
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
	if req == nil {
		return nil, a2atype.ErrInvalidParams
	}
	instance, err := g.taskInstance(ctx, auth.VerbUpdate, req.Tenant, req.ID)
	if err != nil {
		return nil, err
	}
	task, err := g.store.GetAgentInstanceTask(ctx, instance.Id, string(req.ID), nil)
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	if task.Status.State.Terminal() {
		return task, nil
	}
	client, err := g.dial(ctx, instance)
	if err != nil {
		return nil, err
	}
	closeRuntime := sync.OnceValue(client.Destroy)
	defer closeRuntime()
	result, err := client.CancelTask(ctx, req)
	if err != nil {
		_ = closeRuntime()
		if ctx.Err() == nil {
			if recovered, recoverErr := g.recoverBoundary(ctx, instance.Id, req.ID, nil); recoverErr != nil || (recovered != nil && recovered.Status.State.Terminal()) {
				return recovered, recoverErr
			}
		}
		return nil, err
	}
	// Release this observation connection before waiting for native cleanup.
	if err := closeRuntime(); err != nil {
		return nil, err
	}
	if result != nil && isQuiescent(result.Status.State) {
		return g.awaitBoundary(ctx, instance.Id, result.ID, nil)
	}
	return result, nil
}

func (g *Gateway) SendMessage(ctx context.Context, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	var historyLength *int
	if req != nil && req.Config != nil {
		historyLength = req.Config.HistoryLength
	}
	initialID := initialMessageID(req)
	instance, err := g.prepareSend(ctx, req)
	if err != nil {
		return nil, err
	}
	ctx, dispatchID, err := g.reserveDispatch(ctx, instance.Id, initialID)
	if errors.Is(err, database.ErrMessageAccepted) {
		task, err := g.acceptedTask(ctx, instance.Id, initialID, historyLength)
		if err != nil {
			return nil, err
		}
		if req.Config != nil && req.Config.ReturnImmediately {
			return task, nil
		}
		return g.awaitTaskCompletion(ctx, instance.Id, task.ID, historyLength)
	}
	if err != nil {
		return nil, err
	}
	defer g.finishDispatch(ctx, instance.Id, dispatchID, req.Message.ID, nil)
	client, err := g.dial(ctx, instance)
	if err != nil {
		return nil, err
	}
	closeRuntime := sync.OnceValue(client.Destroy)
	defer closeRuntime()
	result, err := client.SendMessage(ctx, req)
	if err != nil {
		err = g.finishDispatch(ctx, instance.Id, dispatchID, req.Message.ID, err)
		_ = closeRuntime()
		// Native settlement can pause the runtime before its unary response is
		// delivered. Recover only this accepted input's durable boundary.
		// The gRPC SDK maps proxy connection failures to A2A InternalError.
		// Preserve explicit protocol rejections, including unwrapped sentinels.
		if ctx.Err() == nil && a2atype.ErrorReason(err) == a2atype.ErrorReason(a2atype.ErrInternalError) {
			if task, readErr := g.store.GetAgentInstanceTaskByMessage(ctx, instance.Id, string(req.Message.TaskID), req.Message.ID); readErr == nil {
				if recovered, recoverErr := g.recoverBoundary(ctx, instance.Id, task.ID, historyLength); recovered != nil || recoverErr != nil {
					return recovered, recoverErr
				}
			}
		}
		return nil, err
	}
	if task, ok := result.(*a2atype.Task); ok && isQuiescent(task.Status.State) {
		if err := closeRuntime(); err != nil {
			return nil, err
		}
		stored, err := g.awaitBoundary(ctx, instance.Id, task.ID, historyLength)
		if err != nil {
			return nil, err
		}
		return stored, nil
	}
	return result, nil
}

func (g *Gateway) SubscribeToTask(ctx context.Context, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	if req == nil {
		return errorEvents(a2atype.ErrInvalidParams)
	}
	instance, err := g.taskInstance(ctx, auth.VerbGet, req.Tenant, req.ID)
	if err != nil {
		return errorEvents(err)
	}
	task, err := g.store.GetAgentInstanceTask(ctx, instance.Id, string(req.ID), nil)
	if err != nil {
		return errorEvents(g.storeError(ctx, err))
	}
	if isQuiescent(task.Status.State) {
		return func(yield func(a2atype.Event, error) bool) { yield(task, nil) }
	}
	client, err := g.dial(ctx, instance)
	if err != nil {
		return errorEvents(err)
	}
	// The SDK subscription supplies its own initial task and later events.
	// Do not concatenate an unrelated stored snapshot with that live stream.
	return g.observe(ctx, instance, req.ID, "", nil, client, client.SubscribeToTask(ctx, req))
}

func (g *Gateway) SendStreamingMessage(ctx context.Context, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	initialID := initialMessageID(req)
	instance, err := g.prepareSend(ctx, req)
	if err != nil {
		return errorEvents(err)
	}
	var historyLength *int
	if req.Config != nil {
		historyLength = req.Config.HistoryLength
	}
	return func(yield func(a2atype.Event, error) bool) {
		ctx, dispatchID, err := g.reserveDispatch(ctx, instance.Id, initialID)
		if errors.Is(err, database.ErrMessageAccepted) {
			task, err := g.acceptedTask(ctx, instance.Id, initialID, historyLength)
			if err != nil {
				yield(nil, err)
				return
			}
			if isQuiescent(task.Status.State) {
				yield(task, nil)
				return
			}
			g.SubscribeToTask(ctx, &a2atype.SubscribeToTaskRequest{Tenant: req.Tenant, ID: task.ID})(yield)
			return
		}
		if err != nil {
			yield(nil, err)
			return
		}
		defer g.finishDispatch(ctx, instance.Id, dispatchID, req.Message.ID, nil)
		client, err := g.dial(ctx, instance)
		if err != nil {
			yield(nil, err)
			return
		}
		events := func(next func(a2atype.Event, error) bool) {
			for event, err := range client.SendStreamingMessage(ctx, req) {
				if err != nil {
					next(nil, g.finishDispatch(ctx, instance.Id, dispatchID, req.Message.ID, err))
					return
				}
				if !next(event, nil) {
					return
				}
			}
			next(nil, g.finishDispatch(ctx, instance.Id, dispatchID, req.Message.ID, a2atype.ErrInternalError))
		}
		g.observe(ctx, instance, req.Message.TaskID, req.Message.ID, historyLength, client, events)(yield)
	}
}

// reserveDispatch waits only before forwarding input. No runtime send is retried
// on an ambiguous transport error, and no SQL lock is held during dispatch.
func (g *Gateway) reserveDispatch(ctx context.Context, instanceID, initialID string) (context.Context, uuid.UUID, error) {
	id := uuid.New()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		err := g.store.ReserveAgentInstanceDispatch(ctx, instanceID, id, initialID)
		if err == nil {
			return a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{apia2a.DispatchHeader: {id.String()}}), id, nil
		}
		if errors.Is(err, database.ErrMessageAccepted) {
			return ctx, uuid.Nil, err
		}
		if !errors.Is(err, database.ErrDispatchBusy) {
			return ctx, uuid.Nil, g.storeError(ctx, err)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx, uuid.Nil, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return ctx, uuid.Nil, sendNotAccepted()
		case <-timer.C:
		}
	}
}

func sendNotAccepted() error {
	return a2atype.NewError(a2atype.ErrUnsupportedOperation, "input was not accepted; retry after the instance becomes available").
		WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED", "retryAfterMs": "100"})
}

func (g *Gateway) finishDispatch(ctx context.Context, instanceID string, id uuid.UUID, messageID string, sendErr error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	revoked, err := g.store.RevokeAgentInstanceDispatch(ctx, instanceID, id, messageID)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "release runtime dispatch", "instance_id", instanceID, "error", err)
	} else if revoked && sendErr != nil && a2atype.ErrorReason(sendErr) == a2atype.ErrorReason(a2atype.ErrInternalError) {
		return sendNotAccepted()
	}
	return sendErr
}

// observe owns only this observer's runtime connection. Losing it cannot cancel
// execution. Final task state becomes visible after native cleanup acknowledges
// the saved version. Publication is independent of runtime pause/suspend.
func (g *Gateway) observe(ctx context.Context, instance *apiv1alpha1.AgentInstance, taskID a2atype.TaskID, messageID string, historyLength *int, client *a2aclient.Client, events iter.Seq2[a2atype.Event, error]) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		closeRuntime := sync.OnceValue(client.Destroy)
		next, stop := iter.Pull2(events)
		defer stop()
		defer closeRuntime()
		var streamErr error
		for {
			event, err, ok := next()
			if !ok {
				break
			}
			if err != nil {
				streamErr = err
				break
			}
			if event == nil || event.TaskInfo().TaskID == "" || event.TaskInfo().ContextID != instance.ContextId || (taskID != "" && event.TaskInfo().TaskID != taskID) {
				yield(nil, a2atype.NewError(a2atype.ErrInternalError, "runtime returned an unexpected task"))
				return
			}
			taskID = event.TaskInfo().TaskID
			var state a2atype.TaskState
			switch value := event.(type) {
			case *a2atype.Task:
				state = value.Status.State
			case *a2atype.TaskStatusUpdateEvent:
				state = value.Status.State
			}
			if isQuiescent(state) {
				if state.Terminal() {
					// Let the runtime finish its response naturally. A stuck stream
					// must not prevent delivery of the persisted task result.
					timer := time.AfterFunc(runtimeDrainTimeout, func() { _ = closeRuntime() })
					for {
						if _, _, ok := next(); !ok {
							break
						}
					}
					timer.Stop()
				}
				if err := closeRuntime(); err != nil {
					yield(nil, err)
					return
				}
				task, err := g.awaitBoundary(ctx, instance.Id, taskID, historyLength)
				yield(task, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
		// Finish can race a subscription attach or stop the runtime stream.
		// Recover only durable public state; an incomplete stream is an error.
		if messageID != "" && ctx.Err() == nil {
			// A continuation's previous waiting boundary is not its response.
			task, err := g.store.GetAgentInstanceTaskByMessage(ctx, instance.Id, string(taskID), messageID)
			if err == nil {
				taskID = task.ID
			} else {
				taskID = ""
			}
		}
		if taskID != "" && ctx.Err() == nil && (streamErr == nil || a2atype.ErrorReason(streamErr) == a2atype.ErrorReason(a2atype.ErrInternalError) || (messageID == "" && errors.Is(streamErr, a2atype.ErrTaskNotFound))) {
			_ = closeRuntime()
			if task, err := g.recoverBoundary(ctx, instance.Id, taskID, historyLength); task != nil || err != nil {
				yield(task, err)
				return
			}
		}
		if streamErr == nil {
			streamErr = a2atype.NewError(a2atype.ErrInternalError, "runtime stream ended before a task boundary")
		}
		yield(nil, streamErr)
	}
}

// recoverBoundary reads a final result after its runtime connection was lost.
// A still-running task has no result to recover; it must not look completed.
func (g *Gateway) recoverBoundary(ctx context.Context, instanceID string, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	task, err := g.store.GetSettledAgentInstanceTask(ctx, instanceID, string(taskID), historyLength)
	if errors.Is(err, database.ErrConflict) {
		return g.awaitBoundary(ctx, instanceID, taskID, historyLength)
	}
	if err == nil && isQuiescent(task.Status.State) {
		return task, nil
	}
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, g.storeError(ctx, err)
	}
	return nil, nil
}

func (g *Gateway) dial(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*a2aclient.Client, error) {
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "connect to runtime", "instance_id", instance.Id, "error", err)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime")
	}
	return client, nil
}

func (g *Gateway) awaitBoundary(ctx context.Context, instanceID string, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		task, err := g.store.GetSettledAgentInstanceTask(ctx, instanceID, string(taskID), historyLength)
		if err == nil {
			return task, nil
		}
		if !errors.Is(err, database.ErrConflict) {
			return nil, g.storeError(ctx, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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

func (g *Gateway) GetExtendedAgentCard(ctx context.Context, req *a2atype.GetExtendedAgentCardRequest) (*a2atype.AgentCard, error) {
	if req == nil {
		return nil, a2atype.ErrInvalidParams
	}
	ref, err := route(ctx, req.Tenant)
	if err != nil {
		return nil, err
	}
	var revisionID string
	if share, ok := auth.ShareContextFrom(ctx); ok {
		instance, err := g.storedInstance(ctx, auth.VerbGet, ref, share.AgentInstanceID)
		if err != nil {
			return nil, err
		}
		revisionID = instance.PreparedRevision
	} else {
		agent, err := g.agents.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name})
		if err != nil {
			return nil, serviceError(ctx, err)
		}
		revisionID = agent.Status.LatestSuccessfulRevision
	}
	if revisionID == "" {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "Agent has no successful revision")
	}
	revision, err := g.store.GetRuntimeRevision(ctx, revisionID)
	if err != nil {
		return nil, a2atype.ErrInternalError
	}
	card, err := apia2a.FromProtoAgentCard(revision.AgentCard)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to decode agent instance agent card", "error", err, "revision", revision.Revision)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load Agent Card")
	}

	// The compiled card provides immutable template metadata. Public transport,
	// security, and signatures belong to the gateway instead of the private
	// runtime that produced that card.
	card.SupportedInterfaces = []*a2atype.AgentInterface{
		a2atype.NewAgentInterface(strings.TrimRight(g.gatewayURL, "/")+HTTPPathPrefix+ref.Namespace+"/"+ref.Name, a2atype.TransportProtocolJSONRPC),
		a2atype.NewAgentInterface(g.gatewayURL, a2atype.TransportProtocolGRPC),
	}
	for _, endpoint := range card.SupportedInterfaces {
		endpoint.Tenant = ref.Namespace + "/" + ref.Name
	}
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

func (g *Gateway) prepareSend(ctx context.Context, req *a2atype.SendMessageRequest) (*apiv1alpha1.AgentInstance, error) {
	if req == nil || req.Message == nil || req.Message.ID == "" {
		return nil, a2atype.ErrInvalidParams
	}
	if req.Config != nil && req.Config.PushConfig != nil {
		return nil, a2atype.ErrPushNotificationNotSupported
	}
	agent, err := route(ctx, req.Tenant)
	if err != nil {
		return nil, err
	}
	apia2a.SanitizeCallerRequest(req)
	var instance *apiv1alpha1.AgentInstance
	switch {
	case req.Message.TaskID != "":
		instance, err = g.taskInstance(ctx, auth.VerbUpdate, req.Tenant, req.Message.TaskID)
	case req.Message.ContextID != "":
		instance, err = g.storedInstance(ctx, auth.VerbCreate, agent, req.Message.ContextID)
	default:
		// Share authority permits access to its conversation, never implicit creation.
		if _, shared := auth.ShareContextFrom(ctx); shared {
			return nil, a2atype.ErrUnauthorized
		}
		if _, err = g.agents.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: agent.Name}); err != nil {
			return nil, serviceError(ctx, err)
		}
		// Instance creation already deduplicates request IDs per authenticated creator.
		requestID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("a2a/"+agent.Namespace+"/"+agent.Name+"/"+req.Message.ID)).String()
		instance, err = g.instances.Create(ctx, agent, requestID, "")
		if err != nil {
			return nil, serviceError(ctx, err)
		}
	}
	if err != nil {
		return nil, err
	}
	if req.Message.ContextID != "" && req.Message.ContextID != instance.ContextId {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message context does not match task")
	}
	req.Message.ContextID = instance.ContextId
	if instance.State != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || instance.Operation != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "AgentInstance cannot accept work during a lifecycle operation")
	}
	return instance, nil
}

func initialMessageID(req *a2atype.SendMessageRequest) string {
	if req != nil && req.Message != nil && req.Message.ContextID == "" && req.Message.TaskID == "" {
		return req.Message.ID
	}
	return ""
}

func requiresInput(state a2atype.TaskState) bool {
	return state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

func isQuiescent(state a2atype.TaskState) bool {
	return state.Terminal() || requiresInput(state)
}

func (g *Gateway) storeError(ctx context.Context, err error) error {
	if errors.Is(err, database.ErrNotFound) {
		return a2atype.ErrTaskNotFound
	}
	if errors.Is(err, database.ErrFailedPrecondition) {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "reply does not match the pending input request")
	}
	if errors.Is(err, database.ErrIdempotencyConflict) {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "message ID was already used with a different request")
	}
	if errors.Is(err, database.ErrConflict) {
		return a2atype.NewError(a2atype.ErrUnsupportedOperation, err.Error())
	}
	logging.FromContext(ctx).ErrorContext(ctx, "read agent instance task", "error", err)
	return a2atype.NewError(a2atype.ErrInternalError, "failed to read task")
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

// acceptedTask recovers a previously admitted first message without dispatching it again.
func (g *Gateway) acceptedTask(ctx context.Context, instanceID, messageID string, historyLength *int) (*a2atype.Task, error) {
	task, err := g.store.GetAgentInstanceTaskByMessage(ctx, instanceID, "", messageID)
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	return shapeTask(task, historyLength, true), nil
}

func (g *Gateway) awaitTaskCompletion(ctx context.Context, instanceID string, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		task, err := g.awaitBoundary(ctx, instanceID, taskID, historyLength)
		if err != nil {
			return nil, err
		}
		if isQuiescent(task.Status.State) {
			return task, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
