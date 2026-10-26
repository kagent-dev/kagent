package grpcserver

import (
	"context"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type scheduledRunServer struct {
	apiv1.UnimplementedScheduledRunServiceServer
	service         *scheduledrun.Service
	maxMessageBytes int
}

func (s *scheduledRunServer) CreateScheduledRun(ctx context.Context, request *apiv1.CreateScheduledRunRequest) (*apiv1.CreateScheduledRunResponse, error) {
	resource, err := s.decodeResource(request.GetRef(), request.GetResource())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Create(ctx, resource)
	if err != nil {
		return nil, err
	}
	response, err := s.resource(result)
	return &apiv1.CreateScheduledRunResponse{ScheduledRun: response}, err
}

func (s *scheduledRunServer) GetScheduledRun(ctx context.Context, request *apiv1.GetScheduledRunRequest) (*apiv1.GetScheduledRunResponse, error) {
	result, err := s.service.Get(ctx, types.NamespacedName{Namespace: request.GetRef().GetNamespace(), Name: request.GetRef().GetName()})
	if err != nil {
		return nil, err
	}
	response, err := s.resource(result)
	return &apiv1.GetScheduledRunResponse{ScheduledRun: response}, err
}

func (s *scheduledRunServer) UpdateScheduledRun(ctx context.Context, request *apiv1.UpdateScheduledRunRequest) (*apiv1.UpdateScheduledRunResponse, error) {
	resource, err := s.decodeResource(request.GetRef(), request.GetResource())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Update(ctx, resource)
	if err != nil {
		return nil, err
	}
	response, err := s.resource(result)
	return &apiv1.UpdateScheduledRunResponse{ScheduledRun: response}, err
}

func (s *scheduledRunServer) DeleteScheduledRun(ctx context.Context, request *apiv1.DeleteScheduledRunRequest) (*apiv1.DeleteScheduledRunResponse, error) {
	err := s.service.Delete(ctx, types.NamespacedName{Namespace: request.GetRef().GetNamespace(), Name: request.GetRef().GetName()}, types.UID(request.GetUid()), request.GetResourceVersion())
	if err != nil {
		return nil, err
	}
	return &apiv1.DeleteScheduledRunResponse{}, nil
}

func (s *scheduledRunServer) ListScheduledRuns(ctx context.Context, request *apiv1.ListScheduledRunsRequest) (*apiv1.ListScheduledRunsResponse, error) {
	resources, next, err := s.service.List(ctx, request.GetNamespace(), request.GetPage())
	if err != nil {
		return nil, err
	}
	result := make([]*apiv1.ScheduledRun, 0, len(resources))
	for _, resource := range resources {
		response, err := s.resource(resource)
		if err != nil {
			return nil, err
		}
		result = append(result, response)
	}
	return &apiv1.ListScheduledRunsResponse{ScheduledRuns: result, Page: &apiv1.PageResponse{NextPageToken: next}}, nil
}

func (s *scheduledRunServer) resource(resource *v1alpha3.ScheduledRun) (*apiv1.ScheduledRun, error) {
	object, err := structuredobject.FromGo(resource, v1alpha3.GroupVersion.String(), v1alpha3.ScheduledRunKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode ScheduledRun", err)
	}
	return &apiv1.ScheduledRun{Ref: &apiv1.ResourceReference{Namespace: resource.Namespace, Name: resource.Name}, Resource: object}, nil
}

func (s *scheduledRunServer) decodeResource(ref *apiv1.ResourceReference, object *apiv1.StructuredObject) (*v1alpha3.ScheduledRun, error) {
	result := &v1alpha3.ScheduledRun{}
	if err := structuredobject.ToGo(object, v1alpha3.ScheduledRunKind, result, s.maxMessageBytes); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid ScheduledRun resource", err)
	}
	if (result.Name != "" && result.Name != ref.GetName()) || (result.Namespace != "" && result.Namespace != ref.GetNamespace()) {
		return nil, serviceerrors.NewInvalidArgument("Reference does not match resource metadata", nil)
	}
	result.Name, result.Namespace = ref.GetName(), ref.GetNamespace()
	return result, nil
}

func (s *scheduledRunServer) TriggerScheduledRun(ctx context.Context, request *apiv1.TriggerScheduledRunRequest) (*apiv1.TriggerScheduledRunResponse, error) {
	result, err := s.service.Trigger(ctx, uuid.MustParse(request.ScheduledRunId), request.RequestId)
	if err != nil {
		return nil, err
	}
	return &apiv1.TriggerScheduledRunResponse{Execution: result}, nil
}

func (s *scheduledRunServer) GetScheduledRunExecution(ctx context.Context, request *apiv1.GetScheduledRunExecutionRequest) (*apiv1.GetScheduledRunExecutionResponse, error) {
	result, err := s.service.GetExecution(ctx, uuid.MustParse(request.GetExecutionId()))
	if err != nil {
		return nil, err
	}
	return &apiv1.GetScheduledRunExecutionResponse{Execution: result}, nil
}

func (s *scheduledRunServer) ListScheduledRunExecutions(ctx context.Context, request *apiv1.ListScheduledRunExecutionsRequest) (*apiv1.ListScheduledRunExecutionsResponse, error) {
	result, next, err := s.service.ListExecutions(ctx, uuid.MustParse(request.GetScheduledRunId()), request.GetPage())
	if err != nil {
		return nil, err
	}
	return &apiv1.ListScheduledRunExecutionsResponse{Executions: result, Page: &apiv1.PageResponse{NextPageToken: next}}, nil
}
