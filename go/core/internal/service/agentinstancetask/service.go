// Package agentinstancetask owns authorized A2A task reads independently of transport
// and runtime execution. The gateway supplies an explicit instance ID, not routing metadata.
package agentinstancetask

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type store interface {
	GetAgentInstanceByID(context.Context, string) (*apiv1alpha1.AgentInstance, error)
	GetAgentInstance(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	GetAgentInstanceTask(context.Context, string, string, *int) (*a2atype.Task, error)
	ListAgentInstanceTasks(context.Context, string, string, a2atype.TaskState, *time.Time, int, *int) ([]*a2atype.Task, int, error)
	GetAgentInstanceTaskObservation(context.Context, string, string, *int) (*database.TaskObservation, error)
	ListAgentInstanceTaskEvents(context.Context, string, string, int64, int) ([]database.TaskEvent, error)
}

var _ store = (*database.Client)(nil)

// Service reads durable tasks with the caller's instance authority. It has no runtime
// dependency, so observation cannot acquire execution ownership or resume an Actor.
type Service struct {
	store      store
	authorizer auth.Authorizer
}

func NewService(store store, authorizer auth.Authorizer) *Service {
	return &Service{store: store, authorizer: authorizer}
}

// Instance resolves a live instance in any lifecycle state under owner/share authority.
// Only authenticated control-plane sessions can read without the owner predicate.
// Callers apply readiness and transactional execution checks when mutating runtime.
func (s *Service) Instance(ctx context.Context, id string, verb auth.Verb) (*apiv1alpha1.AgentInstance, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "invalid AgentInstance ID")
	}
	id = parsed.String()
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, a2atype.NewError(a2atype.ErrUnauthenticated, "authentication is required")
	}
	principal := session.Principal()

	// ShareContext is installed by trusted authentication, never decoded from caller
	// metadata here. It grants access only to the named instance as its owner.
	creator := principal.User.ID
	share, hasShare := auth.ShareContextFrom(ctx)
	if hasShare && share.IsForAgentInstance(id) {
		if share.ReadOnly && verb != auth.VerbGet && verb != auth.VerbList {
			return nil, a2atype.NewError(a2atype.ErrUnauthorized, "share permits read access only")
		}
		creator = share.UserID
	} else if err := s.authorizer.Check(ctx, principal, verb, auth.Resource{Type: "AgentInstance", Name: id}); err != nil {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	var instance *apiv1alpha1.AgentInstance
	// Only the authenticated internal session can read independently of ownership.
	// Authorization above still evaluates the actual control-plane principal.
	if _, controlPlane := session.(auth.ControlPlaneSession); controlPlane && !hasShare {
		instance, err = s.store.GetAgentInstanceByID(ctx, id)
	} else {
		instance, err = s.store.GetAgentInstance(ctx, id, creator)
	}
	if errors.Is(err, database.ErrNotFound) {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "not authorized")
	}
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to load agent instance", "error", err, "instance_id", id)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load AgentInstance")
	}
	return instance, nil
}

// GetTask returns an authorized persisted task, including for suspended instances.
// It never connects to runtime or takes execution ownership.
func (s *Service) GetTask(ctx context.Context, instanceID string, req *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	instance, err := s.Instance(ctx, instanceID, auth.VerbGet)
	if err != nil {
		return nil, err
	}
	if req == nil || req.ID == "" {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "task ID is required")
	}
	task, err := s.store.GetAgentInstanceTask(ctx, instance.GetId(), string(req.ID), req.HistoryLength)
	if errors.Is(err, database.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to load agent instance task", "error", err, "task_id", req.ID)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load task")
	}
	return shapeTask(task, req.HistoryLength, true), nil
}

// ListTasks returns authorized persisted tasks with A2A filtering and pagination.
// It never wakes a suspended instance.
func (s *Service) ListTasks(ctx context.Context, instanceID string, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	instance, err := s.Instance(ctx, instanceID, auth.VerbGet)
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
	if req.ContextID != "" && req.ContextID != instance.GetContextId() {
		return &a2atype.ListTasksResponse{Tasks: []*a2atype.Task{}, PageSize: pageSize}, nil
	}
	afterID, err := decodePageToken(req.PageToken)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "invalid page token")
	}
	tasks, total, err := s.store.ListAgentInstanceTasks(ctx, instance.GetId(), afterID, req.Status, req.StatusTimestampAfter, pageSize+1, req.HistoryLength)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list agent instance tasks", "error", err, "instance_id", instance.GetId())
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
