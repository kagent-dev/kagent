package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
)

// These operations already use upstream protobuf task shapes. The transport
// delegates directly after the shared authentication and validation interceptors.
type taskStoreServer struct {
	apiv1alpha1.UnimplementedTaskStoreServiceServer
	service *taskstore.Service
}

var _ apiv1alpha1.TaskStoreServiceServer = (*taskStoreServer)(nil)

func (s *taskStoreServer) CreateTask(ctx context.Context, req *apiv1alpha1.TaskStoreServiceCreateTaskRequest) (*apiv1alpha1.TaskStoreServiceCreateTaskResponse, error) {
	return s.service.CreateTask(ctx, req)
}

func (s *taskStoreServer) GetTask(ctx context.Context, req *apiv1alpha1.TaskStoreServiceGetTaskRequest) (*apiv1alpha1.TaskStoreServiceGetTaskResponse, error) {
	return s.service.GetTask(ctx, req)
}

func (s *taskStoreServer) UpdateTask(ctx context.Context, req *apiv1alpha1.TaskStoreServiceUpdateTaskRequest) (*apiv1alpha1.TaskStoreServiceUpdateTaskResponse, error) {
	return s.service.UpdateTask(ctx, req)
}

func (s *taskStoreServer) ListTasks(ctx context.Context, req *apiv1alpha1.TaskStoreServiceListTasksRequest) (*apiv1alpha1.TaskStoreServiceListTasksResponse, error) {
	return s.service.ListTasks(ctx, req)
}

func (s *taskStoreServer) SettleTask(ctx context.Context, req *apiv1alpha1.TaskStoreServiceSettleTaskRequest) (*apiv1alpha1.TaskStoreServiceSettleTaskResponse, error) {
	return s.service.SettleTask(ctx, req)
}
