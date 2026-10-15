package grpcserver

import (
	"context"

	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
)

// These operations already use upstream protobuf task shapes. The transport
// delegates directly after the shared authentication and validation interceptors.
type taskStoreServer struct {
	runtimev1.UnimplementedTaskStoreServiceServer
	service *taskstore.Service
}

var _ runtimev1.TaskStoreServiceServer = (*taskStoreServer)(nil)

func (s *taskStoreServer) CreateTask(ctx context.Context, req *runtimev1.TaskStoreServiceCreateTaskRequest) (*runtimev1.TaskStoreServiceCreateTaskResponse, error) {
	return s.service.CreateTask(ctx, req)
}

func (s *taskStoreServer) GetTask(ctx context.Context, req *runtimev1.TaskStoreServiceGetTaskRequest) (*runtimev1.TaskStoreServiceGetTaskResponse, error) {
	return s.service.GetTask(ctx, req)
}

func (s *taskStoreServer) UpdateTask(ctx context.Context, req *runtimev1.TaskStoreServiceUpdateTaskRequest) (*runtimev1.TaskStoreServiceUpdateTaskResponse, error) {
	return s.service.UpdateTask(ctx, req)
}

func (s *taskStoreServer) ListTasks(ctx context.Context, req *runtimev1.TaskStoreServiceListTasksRequest) (*runtimev1.TaskStoreServiceListTasksResponse, error) {
	return s.service.ListTasks(ctx, req)
}

func (s *taskStoreServer) SettleTask(ctx context.Context, req *runtimev1.TaskStoreServiceSettleTaskRequest) (*runtimev1.TaskStoreServiceSettleTaskResponse, error) {
	return s.service.SettleTask(ctx, req)
}
