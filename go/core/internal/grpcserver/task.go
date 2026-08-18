package grpcserver

import (
	"context"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	taskservice "github.com/kagent-dev/kagent/go/core/internal/service/task"
)

type taskServer struct {
	apiv1alpha1.UnimplementedTaskStoreServiceServer
	service *taskservice.Service
}

func newTaskServer(service *taskservice.Service) *taskServer {
	return &taskServer{service: service}
}

func (s *taskServer) UpsertTask(ctx context.Context, request *apiv1alpha1.UpsertTaskRequest) (*apiv1alpha1.UpsertTaskResponse, error) {
	task, err := pbconv.FromProtoTask(request.GetTask())
	if err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid task payload", err)
	}
	_, err = s.service.Create(ctx, task)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpsertTaskResponse{}, nil
}

func (s *taskServer) GetTask(ctx context.Context, request *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
	task, err := s.service.Get(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	encoded, err := taskToProto(task)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetTaskResponse{Task: encoded}, nil
}

func (s *taskServer) DeleteTask(ctx context.Context, request *apiv1alpha1.DeleteTaskRequest) (*apiv1alpha1.DeleteTaskResponse, error) {
	if err := s.service.Delete(ctx, request.GetTaskId()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteTaskResponse{}, nil
}

func (s *taskServer) ListTasks(ctx context.Context, request *apiv1alpha1.ListTasksRequest) (*apiv1alpha1.ListTasksResponse, error) {
	values, err := s.service.List(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	tasks := make([]*a2apb.Task, 0, len(values))
	for _, value := range values {
		encoded, err := taskToProto(value)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, encoded)
	}
	return &apiv1alpha1.ListTasksResponse{Tasks: tasks}, nil
}

func taskToProto(value *a2a.Task) (*a2apb.Task, error) {
	encoded, err := pbconv.ToProtoTask(value)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode task", err)
	}
	return encoded, nil
}
