// Package a2agateway exposes AgentInstances through the upstream A2A service.
// The initial handler establishes authenticated routing; the durable public
// Task and event layer will wrap runtime calls here rather than enter the gRPC
// transport or binary wiring.
package a2agateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
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
)

type instanceStore interface {
	GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error)
	CreateAgentInstanceTask(context.Context, string, []byte, *a2atype.Task) (*a2atype.Task, bool, error)
	StoreAgentInstanceTaskEvent(context.Context, string, *a2atype.Task, a2atype.Event) error
	GetAgentInstanceTask(context.Context, string, string) (*a2atype.Task, error)
	ListAgentInstanceTasks(context.Context, string, string, a2atype.TaskState, *time.Time, int) ([]*a2atype.Task, int, error)
}

type runtimeDialer interface {
	Dial(context.Context, *apiv1alpha1.AgentInstance) (*a2aclient.Client, error)
}

// Gateway is transport-neutral. The v0 deployment registers it on the
// controller's gRPC server, while a standalone gateway can register the same
// handler on its own server later.
type Gateway struct {
	store      instanceStore
	authorizer auth.Authorizer
	dialer     runtimeDialer
}

var _ a2asrv.RequestHandler = (*Gateway)(nil)

// New returns the upstream A2A handler independently of any listener or gRPC
// server, keeping deployment topology outside the gateway package.
func New(store instanceStore, authorizer auth.Authorizer, dialer runtimeDialer) a2asrv.RequestHandler {
	return &a2asrv.InterceptedHandler{
		Handler:      &Gateway{store: store, authorizer: authorizer, dialer: dialer},
		Interceptors: []a2asrv.CallInterceptor{a2aext.NewServerPropagator(nil)},
	}
}

func (g *Gateway) instance(ctx context.Context, verb auth.Verb) (*apiv1alpha1.AgentInstance, error) {
	namespace, id, err := route(ctx)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, err.Error())
	}
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, a2atype.NewError(a2atype.ErrUnauthenticated, "authentication is required")
	}
	principal := session.Principal()
	if err := g.authorizer.Check(ctx, principal, verb, auth.Resource{Type: "AgentInstance", Name: namespace + "/" + id}); err != nil {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	instance, err := g.store.GetAgentInstance(ctx, namespace, id, principal.User.ID)
	if errors.Is(err, dbpkg.ErrNotFound) {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to load AgentInstance", "namespace", namespace, "id", id)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load AgentInstance")
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, fmt.Sprintf("AgentInstance is %s", instance.GetState()))
	}
	return instance, nil
}

func (g *Gateway) runtime(ctx context.Context, verb auth.Verb) (*a2aclient.Client, error) {
	instance, err := g.instance(ctx, verb)
	if err != nil {
		return nil, err
	}
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to connect to AgentInstance runtime", "namespace", instance.GetNamespace(), "id", instance.GetId())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime")
	}
	return client, nil
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
	instance, err := g.instance(ctx, auth.VerbGet)
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
	instance, err := g.instance(ctx, auth.VerbGet)
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
	client, err := g.runtime(ctx, auth.VerbUpdate)
	if err != nil {
		return nil, err
	}
	defer client.Destroy()
	return client.CancelTask(ctx, req)
}

func (g *Gateway) SendMessage(ctx context.Context, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	instance, submitted, created, err := g.prepareSend(ctx, req)
	if err != nil {
		return nil, err
	}
	if !created {
		return submitted, nil
	}
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		g.failTask(ctx, instance.GetId(), submitted)
		ctrllog.FromContext(ctx).Error(err, "failed to connect to AgentInstance runtime", "instance", instance.GetId())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime")
	}
	defer client.Destroy()
	result, err := client.SendMessage(ctx, req)
	if err != nil {
		g.failTask(ctx, instance.GetId(), submitted)
		return nil, err
	}
	task, err := taskForResult(submitted, result)
	if err != nil {
		g.failTask(ctx, instance.GetId(), submitted)
		return nil, err
	}
	if err := g.store.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), task, result); err != nil {
		g.failTask(ctx, instance.GetId(), submitted)
		return nil, g.storeError(ctx, err)
	}
	return result, nil
}

func (g *Gateway) SubscribeToTask(ctx context.Context, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	client, err := g.runtime(ctx, auth.VerbGet)
	if err != nil {
		return errorEvents(err)
	}
	return closeAfter(client, client.SubscribeToTask(ctx, req))
}

func (g *Gateway) SendStreamingMessage(ctx context.Context, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	instance, submitted, created, err := g.prepareSend(ctx, req)
	if err != nil {
		return errorEvents(err)
	}
	if !created {
		return func(yield func(a2atype.Event, error) bool) { yield(submitted, nil) }
	}
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		g.failTask(ctx, instance.GetId(), submitted)
		ctrllog.FromContext(ctx).Error(err, "failed to connect to AgentInstance runtime", "instance", instance.GetId())
		return errorEvents(a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime"))
	}
	return func(yield func(a2atype.Event, error) bool) {
		defer client.Destroy()
		for event, eventErr := range client.SendStreamingMessage(ctx, req) {
			if eventErr != nil {
				g.failTask(ctx, instance.GetId(), submitted)
				yield(nil, eventErr)
				return
			}
			task, err := g.taskForEvent(ctx, client, instance.GetId(), submitted, event)
			if err == nil {
				err = g.store.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), task, event)
			}
			if err != nil {
				g.failTask(ctx, instance.GetId(), submitted)
				yield(nil, g.storeError(ctx, err))
				return
			}
			if !yield(event, nil) {
				return
			}
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
	return nil, a2atype.ErrExtendedCardNotConfigured
}

func (g *Gateway) prepareSend(ctx context.Context, req *a2atype.SendMessageRequest) (*apiv1alpha1.AgentInstance, *a2atype.Task, bool, error) {
	instance, err := g.instance(ctx, auth.VerbCreate)
	if err != nil {
		return nil, nil, false, err
	}
	if req == nil || req.Message == nil {
		return nil, nil, false, a2atype.NewError(a2atype.ErrInvalidRequest, "message is required")
	}
	if req.Message.ID == "" {
		return nil, nil, false, a2atype.NewError(a2atype.ErrInvalidRequest, "message ID is required")
	}
	if req.Message.ContextID != "" && req.Message.ContextID != instance.GetId() {
		return nil, nil, false, a2atype.NewError(a2atype.ErrInvalidRequest, "message context does not match AgentInstance")
	}
	if req.Message.TaskID != "" {
		return nil, nil, false, a2atype.NewError(a2atype.ErrUnsupportedOperation, "continuing a task is not supported")
	}
	req.Message.ContextID = instance.GetId()
	requestHash, err := hashSendRequest(req)
	if err != nil {
		return nil, nil, false, a2atype.NewError(a2atype.ErrInvalidRequest, "message cannot be encoded")
	}
	req.Message.TaskID = a2atype.NewTaskID()
	submitted := a2atype.NewSubmittedTask(req.Message, req.Message)
	stored, created, err := g.store.CreateAgentInstanceTask(ctx, instance.GetId(), requestHash, submitted)
	if err != nil {
		return nil, nil, false, g.storeError(ctx, err)
	}
	return instance, stored, created, nil
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

func (g *Gateway) taskForEvent(ctx context.Context, client *a2aclient.Client, instanceID string, submitted *a2atype.Task, event a2atype.Event) (*a2atype.Task, error) {
	if event == nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, "runtime returned an empty event")
	}
	if message, ok := event.(*a2atype.Message); ok {
		if message.TaskID == "" {
			message.TaskID = submitted.ID
		}
		if message.ContextID == "" {
			message.ContextID = submitted.ContextID
		}
	}
	if err := validateTaskInfo(event, submitted); err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, err.Error())
	}
	if task, ok := event.(*a2atype.Task); ok {
		return task, nil
	}
	if message, ok := event.(*a2atype.Message); ok {
		task, err := g.store.GetAgentInstanceTask(ctx, instanceID, string(submitted.ID))
		if err != nil {
			return nil, err
		}
		copy := *task
		copy.History = append(append([]*a2atype.Message{}, task.History...), message)
		now := time.Now()
		copy.Status = a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &now}
		return &copy, nil
	}
	// The private runtime already folds status and artifact events; persist its projection.
	task, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: submitted.ID})
	if err != nil {
		return nil, fmt.Errorf("load runtime task projection: %w", err)
	}
	if err := validateTaskInfo(task, submitted); err != nil {
		return nil, a2atype.NewError(a2atype.ErrInternalError, err.Error())
	}
	return task, nil
}

func validateTaskInfo(value a2atype.TaskInfoProvider, expected *a2atype.Task) error {
	info := value.TaskInfo()
	if info.TaskID != expected.ID || info.ContextID != expected.ContextID {
		return fmt.Errorf("runtime returned mismatched task identity")
	}
	return nil
}

func (g *Gateway) failTask(ctx context.Context, instanceID string, task *a2atype.Task) {
	now := time.Now()
	failed := *task
	failed.Status = a2atype.TaskStatus{State: a2atype.TaskStateFailed, Timestamp: &now}
	if err := g.store.StoreAgentInstanceTaskEvent(ctx, instanceID, &failed, &failed); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to record failed AgentInstance task", "task", task.ID)
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

func closeAfter(client *a2aclient.Client, events iter.Seq2[a2atype.Event, error]) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		defer client.Destroy()
		for event, err := range events {
			if !yield(event, err) {
				return
			}
		}
	}
}
