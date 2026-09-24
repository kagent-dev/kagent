// Package taskstore owns the private persistence API used by runtime SDKs.
// Execution stays in the runtime; public access checks stay in the public API.
package taskstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type Store interface {
	SettleAgentInstanceTask(context.Context, string, string, int64) error
	ClaimTaskFinalization(context.Context) (*database.TaskFinalization, error)
	PublishTaskBoundary(context.Context, *database.TaskFinalization, *database.AgentInstanceTaskSnapshot) error
	GetAgentInstanceForRuntime(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	CreateRuntimeTask(context.Context, string, []byte, *a2a.Task) (int64, error)
	GetVersionedAgentInstanceTask(context.Context, string, string) (*a2a.Task, int64, error)
	UpdateAgentInstanceTask(context.Context, string, int64, []byte, *a2a.Task, a2a.Event) (int64, error)
	ListAgentInstanceTasks(context.Context, string, string, a2a.TaskState, *time.Time, int, *int) ([]*a2a.Task, int, error)
}

var _ Store = (*database.Client)(nil)

type Service struct {
	store    Store
	workflow BoundaryWorkflow
	wake     chan struct{}
}

func NewService(store Store, workflow BoundaryWorkflow) *Service {
	return &Service{store: store, workflow: workflow, wake: make(chan struct{}, 4)}
}

func (s *Service) instance(ctx context.Context, instanceID string) (*apiv1alpha1.AgentInstance, error) {
	session, _ := auth.AuthSessionFrom(ctx)
	identity, ok := session.(runtimeSession)
	if !ok || identity.instanceID != instanceID {
		return nil, status.Error(codes.PermissionDenied, "actor does not belong to this instance")
	}
	instance, err := s.store.GetAgentInstanceForRuntime(ctx, instanceID, identity.actorUID)
	if err != nil {
		return nil, storageError(err)
	}
	if instance.A2AAuthority != substrate.ActorHost(identity.atespace, substrate.ActorName(instanceID), "") {
		return nil, status.Error(codes.PermissionDenied, "actor does not belong to this instance")
	}
	return instance, nil
}

// CreateTask persists the SDK's first task snapshot. Runtime execution and
// request serialization remain owned by the agent's SDK.
func (s *Service) CreateTask(ctx context.Context, input *apiv1alpha1.TaskStoreServiceCreateTaskRequest) (*apiv1alpha1.TaskStoreServiceCreateTaskResponse, error) {
	if _, err := s.instance(ctx, input.AgentInstanceId); err != nil {
		return nil, err
	}
	task, err := pbconv.FromProtoTask(input.Task)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	version, err := s.store.CreateRuntimeTask(ctx, input.AgentInstanceId, hash[:], task)
	return &apiv1alpha1.TaskStoreServiceCreateTaskResponse{Version: version}, storageError(err)
}

func (s *Service) GetTask(ctx context.Context, input *apiv1alpha1.TaskStoreServiceGetTaskRequest) (*apiv1alpha1.TaskStoreServiceGetTaskResponse, error) {
	if _, err := s.instance(ctx, input.AgentInstanceId); err != nil {
		return nil, err
	}
	stored, err := s.getTask(ctx, input.AgentInstanceId, input.TaskId)
	return &apiv1alpha1.TaskStoreServiceGetTaskResponse{Stored: stored}, err
}

func (s *Service) getTask(ctx context.Context, instanceID, taskID string) (*apiv1alpha1.StoredTask, error) {
	task, version, err := s.store.GetVersionedAgentInstanceTask(ctx, instanceID, taskID)
	if err != nil {
		return nil, storageError(err)
	}
	wire, err := pbconv.ToProtoTask(task)
	return &apiv1alpha1.StoredTask{Task: wire, Version: version}, err
}

func (s *Service) UpdateTask(ctx context.Context, input *apiv1alpha1.TaskStoreServiceUpdateTaskRequest) (*apiv1alpha1.TaskStoreServiceUpdateTaskResponse, error) {
	if _, err := s.instance(ctx, input.AgentInstanceId); err != nil {
		return nil, err
	}
	task, err := pbconv.FromProtoTask(input.Task)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	var event a2a.Event = task
	if input.Event != nil {
		event, err = pbconv.FromProtoStreamResponse(input.Event)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	version, err := s.store.UpdateAgentInstanceTask(ctx, input.AgentInstanceId, input.ExpectedVersion, hash[:], task, event)
	if err == nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return &apiv1alpha1.TaskStoreServiceUpdateTaskResponse{Version: version}, storageError(err)
}

func (s *Service) ListTasks(ctx context.Context, input *apiv1alpha1.TaskStoreServiceListTasksRequest) (*apiv1alpha1.TaskStoreServiceListTasksResponse, error) {
	instance, err := s.instance(ctx, input.AgentInstanceId)
	if err != nil {
		return nil, err
	}
	request, err := pbconv.FromProtoListTasksRequest(input.Request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if request.ContextID != "" && request.ContextID != instance.ContextId {
		return nil, status.Error(codes.InvalidArgument, "context does not match AgentInstance")
	}
	after, err := base64.RawURLEncoding.DecodeString(request.PageToken)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid page token")
	}
	limit := 50
	if request.PageSize != 0 {
		limit = request.PageSize
	}
	if limit < 1 || limit > 100 {
		return nil, status.Error(codes.InvalidArgument, "page size must be between 1 and 100")
	}
	tasks, total, err := s.store.ListAgentInstanceTasks(ctx, instance.Id, string(after), request.Status, request.StatusTimestampAfter, limit+1, request.HistoryLength)
	if err != nil {
		return nil, storageError(err)
	}
	result := &a2a.ListTasksResponse{Tasks: tasks, PageSize: limit, TotalSize: total}
	if len(tasks) > limit {
		result.NextPageToken = base64.RawURLEncoding.EncodeToString([]byte(tasks[limit-1].ID))
		result.Tasks = tasks[:limit]
	}
	if !request.IncludeArtifacts {
		for _, task := range result.Tasks {
			task.Artifacts = nil
		}
	}
	wire, err := pbconv.ToProtoListTasksResponse(result)
	if err != nil {
		return nil, fmt.Errorf("encode task list: %w", err)
	}
	return &apiv1alpha1.TaskStoreServiceListTasksResponse{Result: wire}, nil
}

func (s *Service) SettleTask(ctx context.Context, input *apiv1alpha1.TaskStoreServiceSettleTaskRequest) (*apiv1alpha1.TaskStoreServiceSettleTaskResponse, error) {
	if _, err := s.instance(ctx, input.AgentInstanceId); err != nil {
		return nil, err
	}
	if err := s.store.SettleAgentInstanceTask(ctx, input.AgentInstanceId, input.TaskId, input.Version); err != nil {
		return nil, storageError(err)
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return &apiv1alpha1.TaskStoreServiceSettleTaskResponse{}, nil
}

func storageError(err error) error {
	switch {
	case errors.Is(err, database.ErrNotFound):
		return status.Error(codes.NotFound, "instance or task does not exist")
	case errors.Is(err, database.ErrIdempotencyConflict):
		return status.Error(codes.AlreadyExists, "task ID already exists with another creation")
	case errors.Is(err, database.ErrConflict):
		return status.Error(codes.Aborted, "task changed or the instance cannot accept this update")
	case errors.Is(err, database.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, "task state does not allow this operation")
	default:
		return err
	}
}
