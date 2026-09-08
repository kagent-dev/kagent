package grpcserver

import (
	"context"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
)

// Scheduling request IDs are validated by the shared Protovalidate interceptor
// before these handlers convert them to UUIDs for the service.
type scheduledRunServer struct {
	apiv1alpha1.UnimplementedScheduledRunServiceServer
	service *scheduledrun.Service
}

func (s *scheduledRunServer) CreateScheduledRun(ctx context.Context, request *apiv1alpha1.CreateScheduledRunRequest) (*apiv1alpha1.CreateScheduledRunResponse, error) {
	result, err := s.service.Create(ctx, scheduledrun.CreateRequest{Harness: request.Harness, AgentTemplate: request.AgentTemplate, RequestID: request.RequestId, Config: request.Config})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateScheduledRunResponse{ScheduledRun: result}, nil
}

func (s *scheduledRunServer) GetScheduledRun(ctx context.Context, request *apiv1alpha1.GetScheduledRunRequest) (*apiv1alpha1.GetScheduledRunResponse, error) {
	result, err := s.service.Get(ctx, uuid.MustParse(request.ScheduledRunId))
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetScheduledRunResponse{ScheduledRun: result}, nil
}

func (s *scheduledRunServer) UpdateScheduledRun(ctx context.Context, request *apiv1alpha1.UpdateScheduledRunRequest) (*apiv1alpha1.UpdateScheduledRunResponse, error) {
	result, err := s.service.Update(ctx, uuid.MustParse(request.ScheduledRunId), request.Etag, request.Config)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdateScheduledRunResponse{ScheduledRun: result}, nil
}

func (s *scheduledRunServer) DeleteScheduledRun(ctx context.Context, request *apiv1alpha1.DeleteScheduledRunRequest) (*apiv1alpha1.DeleteScheduledRunResponse, error) {
	result, err := s.service.Delete(ctx, uuid.MustParse(request.ScheduledRunId))
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteScheduledRunResponse{ScheduledRun: result}, nil
}

func (s *scheduledRunServer) TriggerScheduledRun(ctx context.Context, request *apiv1alpha1.TriggerScheduledRunRequest) (*apiv1alpha1.TriggerScheduledRunResponse, error) {
	result, err := s.service.Trigger(ctx, uuid.MustParse(request.ScheduledRunId), request.RequestId)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.TriggerScheduledRunResponse{Execution: result}, nil
}

func (s *scheduledRunServer) ListScheduledRuns(ctx context.Context, request *apiv1alpha1.ListScheduledRunsRequest) (*apiv1alpha1.ListScheduledRunsResponse, error) {
	result, next, err := s.service.List(ctx, request.GetPage())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.ListScheduledRunsResponse{ScheduledRuns: result, Page: &apiv1alpha1.PageResponse{NextPageToken: next}}, nil
}

func (s *scheduledRunServer) GetScheduledRunExecution(ctx context.Context, request *apiv1alpha1.GetScheduledRunExecutionRequest) (*apiv1alpha1.GetScheduledRunExecutionResponse, error) {
	result, err := s.service.GetExecution(ctx, uuid.MustParse(request.GetExecutionId()))
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetScheduledRunExecutionResponse{Execution: result}, nil
}

func (s *scheduledRunServer) ListScheduledRunExecutions(ctx context.Context, request *apiv1alpha1.ListScheduledRunExecutionsRequest) (*apiv1alpha1.ListScheduledRunExecutionsResponse, error) {
	result, next, err := s.service.ListExecutions(ctx, uuid.MustParse(request.GetScheduledRunId()), request.GetPage())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.ListScheduledRunExecutionsResponse{Executions: result, Page: &apiv1alpha1.PageResponse{NextPageToken: next}}, nil
}
