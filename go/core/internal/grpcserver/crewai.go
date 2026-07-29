package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	crewaiservice "github.com/kagent-dev/kagent/go/core/internal/service/crewai"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	crewAIAPIVersion        = "kagent.api/v1alpha1"
	crewAIMemoryDataKind    = "CrewAIMemoryData"
	crewAIFlowStateDataKind = "CrewAIFlowStateData"
)

type crewAIServer struct {
	apiv1alpha1.UnimplementedCrewAIServiceServer
	service         *crewaiservice.Service
	maxMessageBytes int
}

func newCrewAIServer(service *crewaiservice.Service, maxMessageBytes int) *crewAIServer {
	return &crewAIServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *crewAIServer) StoreMemory(ctx context.Context, request *apiv1alpha1.StoreMemoryRequest) (*apiv1alpha1.StoreMemoryResponse, error) {
	data, err := s.decodeObject(request.GetMemoryData(), crewAIMemoryDataKind, "Invalid CrewAI memory data")
	if err != nil {
		return nil, err
	}
	if err := s.service.StoreMemory(ctx, request.GetThreadId(), data); err != nil {
		return nil, err
	}
	return &apiv1alpha1.StoreMemoryResponse{}, nil
}

func (s *crewAIServer) GetMemory(ctx context.Context, request *apiv1alpha1.GetMemoryRequest) (*apiv1alpha1.GetMemoryResponse, error) {
	limit := 0
	if request.Limit != nil {
		limit = int(request.GetLimit())
	}
	values, err := s.service.GetMemory(ctx, request.GetThreadId(), request.GetTaskDescription(), limit)
	if err != nil {
		return nil, err
	}

	memories := make([]*apiv1alpha1.CrewAIMemory, 0, len(values))
	for _, value := range values {
		data, err := structuredobject.FromGo(value.Data, crewAIAPIVersion, crewAIMemoryDataKind, s.maxMessageBytes)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to encode CrewAI memory data", err)
		}
		memories = append(memories, &apiv1alpha1.CrewAIMemory{
			ThreadId:   value.ThreadID,
			UserId:     value.UserID,
			MemoryData: data,
		})
	}
	return &apiv1alpha1.GetMemoryResponse{Memories: memories}, nil
}

func (s *crewAIServer) ResetMemory(ctx context.Context, request *apiv1alpha1.ResetMemoryRequest) (*apiv1alpha1.ResetMemoryResponse, error) {
	if err := s.service.ResetMemory(ctx, request.GetThreadId()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.ResetMemoryResponse{}, nil
}

func (s *crewAIServer) StoreFlowState(ctx context.Context, request *apiv1alpha1.StoreFlowStateRequest) (*apiv1alpha1.StoreFlowStateResponse, error) {
	data, err := s.decodeObject(request.GetStateData(), crewAIFlowStateDataKind, "Invalid CrewAI flow state data")
	if err != nil {
		return nil, err
	}
	if err := s.service.StoreFlowState(ctx, &crewaiservice.FlowState{
		ThreadID:   request.GetThreadId(),
		MethodName: request.GetMethodName(),
		Data:       data,
	}); err != nil {
		return nil, err
	}
	return &apiv1alpha1.StoreFlowStateResponse{}, nil
}

func (s *crewAIServer) GetFlowState(ctx context.Context, request *apiv1alpha1.GetFlowStateRequest) (*apiv1alpha1.GetFlowStateResponse, error) {
	value, err := s.service.GetFlowState(ctx, request.GetThreadId())
	if err != nil {
		return nil, err
	}
	data, err := structuredobject.FromGo(value.Data, crewAIAPIVersion, crewAIFlowStateDataKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode CrewAI flow state data", err)
	}
	return &apiv1alpha1.GetFlowStateResponse{State: &apiv1alpha1.CrewAIFlowState{
		ThreadId:   value.ThreadID,
		MethodName: value.MethodName,
		StateData:  data,
	}}, nil
}

func (s *crewAIServer) decodeObject(object *apiv1alpha1.StructuredObject, kind, message string) (map[string]any, error) {
	data := map[string]any{}
	if err := structuredobject.ToGo(object, kind, &data, s.maxMessageBytes); err != nil {
		return nil, serviceerrors.NewInvalidArgument(message, err)
	}
	return data, nil
}
