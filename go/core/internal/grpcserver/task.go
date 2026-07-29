package grpcserver

import (
	"context"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	taskservice "github.com/kagent-dev/kagent/go/core/internal/service/task"
)

const (
	a2aTaskAPIVersion = "lf.a2a.v1"
	a2aTaskKind       = "Task"
)

type taskServer struct {
	apiv1alpha1.UnimplementedTaskServiceServer
	service         *taskservice.Service
	maxMessageBytes int
}

func newTaskServer(service *taskservice.Service, maxMessageBytes int) *taskServer {
	return &taskServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *taskServer) CreateTask(ctx context.Context, request *apiv1alpha1.CreateTaskRequest) (*apiv1alpha1.CreateTaskResponse, error) {
	task := &a2a.Task{}
	if err := structuredobject.ToGo(request.GetTask(), a2aTaskKind, task, s.maxMessageBytes); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid task payload", err)
	}
	created, err := s.service.Create(ctx, task)
	if err != nil {
		return nil, err
	}
	encoded, err := taskToStructuredObject(created, s.maxMessageBytes)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateTaskResponse{Task: encoded}, nil
}

func (s *taskServer) GetTask(ctx context.Context, request *apiv1alpha1.GetTaskRequest) (*apiv1alpha1.GetTaskResponse, error) {
	task, err := s.service.Get(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	encoded, err := taskToStructuredObject(task, s.maxMessageBytes)
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
	tasks := make([]*apiv1alpha1.StructuredObject, 0, len(values))
	for _, value := range values {
		encoded, err := taskToStructuredObject(value, s.maxMessageBytes)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, encoded)
	}
	return &apiv1alpha1.ListTasksResponse{Tasks: tasks}, nil
}

func taskToStructuredObject(value *a2a.Task, maxMessageBytes int) (*apiv1alpha1.StructuredObject, error) {
	encoded, err := structuredobject.FromGo(value, a2aTaskAPIVersion, a2aTaskKind, maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode task", err)
	}
	return encoded, nil
}
