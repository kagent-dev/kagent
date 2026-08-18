package grpcserver

import (
	"context"
	"encoding/json"

	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	memoryservice "github.com/kagent-dev/kagent/go/core/internal/service/memory"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type memoryServer struct {
	apiv1alpha1.UnimplementedMemoryServiceServer
	service *memoryservice.Service
}

func newMemoryServer(service *memoryservice.Service) *memoryServer {
	return &memoryServer{service: service}
}

func (s *memoryServer) AddSession(ctx context.Context, request *apiv1alpha1.MemoryServiceAddSessionRequest) (*apiv1alpha1.MemoryServiceAddSessionResponse, error) {
	input, err := memoryInputFromProto(request.GetMemory())
	if err != nil {
		return nil, err
	}
	id, err := s.service.Add(ctx, input)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.MemoryServiceAddSessionResponse{Id: id}, nil
}

func (s *memoryServer) AddSessionBatch(ctx context.Context, request *apiv1alpha1.MemoryServiceAddSessionBatchRequest) (*apiv1alpha1.MemoryServiceAddSessionBatchResponse, error) {
	inputs := make([]memoryservice.Input, 0, len(request.GetItems()))
	for _, item := range request.GetItems() {
		input, err := memoryInputFromProto(item)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	count, err := s.service.AddBatch(ctx, inputs)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.MemoryServiceAddSessionBatchResponse{Count: int32(count)}, nil
}

func (s *memoryServer) Search(ctx context.Context, request *apiv1alpha1.MemoryServiceSearchRequest) (*apiv1alpha1.MemoryServiceSearchResponse, error) {
	results, err := s.service.Search(ctx, memoryservice.SearchRequest{
		AgentName: request.GetAgentName(),
		UserID:    request.GetUserId(),
		Vector:    request.GetVector(),
		Limit:     int(request.GetLimit()),
		MinScore:  request.GetMinScore(),
	})
	if err != nil {
		return nil, err
	}

	memories := make([]*apiv1alpha1.MemorySearchResult, 0, len(results))
	for _, result := range results {
		metadata, err := memoryMetadataToProto(result.Metadata)
		if err != nil {
			return nil, err
		}
		memory := &apiv1alpha1.MemorySearchResult{
			Id:       result.ID,
			Content:  result.Content,
			Score:    result.Score,
			Metadata: metadata,
		}
		if !result.CreatedAt.IsZero() {
			memory.CreatedAt = timestamppb.New(result.CreatedAt)
		}
		memories = append(memories, memory)
	}
	return &apiv1alpha1.MemoryServiceSearchResponse{Memories: memories}, nil
}

func (s *memoryServer) List(ctx context.Context, request *apiv1alpha1.MemoryServiceListRequest) (*apiv1alpha1.MemoryServiceListResponse, error) {
	results, err := s.service.List(ctx, request.GetAgentName(), request.GetUserId())
	if err != nil {
		return nil, err
	}

	memories := make([]*apiv1alpha1.MemorySummary, 0, len(results))
	for index := range results {
		memories = append(memories, memorySummaryToProto(&results[index]))
	}
	return &apiv1alpha1.MemoryServiceListResponse{Memories: memories}, nil
}

func (s *memoryServer) Delete(ctx context.Context, request *apiv1alpha1.MemoryServiceDeleteRequest) (*apiv1alpha1.MemoryServiceDeleteResponse, error) {
	if err := s.service.Delete(ctx, request.GetAgentName(), request.GetUserId()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.MemoryServiceDeleteResponse{Status: "deleted"}, nil
}

func memoryInputFromProto(input *apiv1alpha1.SessionMemoryInput) (memoryservice.Input, error) {
	if input == nil {
		return memoryservice.Input{}, nil
	}
	var metadata json.RawMessage
	if input.GetMetadata() != nil {
		encoded, err := json.Marshal(input.GetMetadata().AsMap())
		if err != nil {
			return memoryservice.Input{}, serviceerrors.NewInvalidArgument("metadata is invalid", err)
		}
		metadata = encoded
	}
	return memoryservice.Input{
		AgentName: input.GetAgentName(),
		UserID:    input.GetUserId(),
		Content:   input.GetContent(),
		Vector:    input.GetVector(),
		Metadata:  metadata,
		TTLDays:   int(input.GetTtlDays()),
	}, nil
}

func memoryMetadataToProto(metadata json.RawMessage) (*structpb.Struct, error) {
	values := make(map[string]any)
	if err := json.Unmarshal(metadata, &values); err != nil {
		return nil, serviceerrors.NewInternal("failed to encode memory metadata", err)
	}
	result, err := structpb.NewStruct(values)
	if err != nil {
		return nil, serviceerrors.NewInternal("failed to encode memory metadata", err)
	}
	return result, nil
}

func memorySummaryToProto(memory *database.Memory) *apiv1alpha1.MemorySummary {
	result := &apiv1alpha1.MemorySummary{
		Id:          memory.ID,
		Content:     memory.Content,
		AccessCount: memory.AccessCount,
	}
	if !memory.CreatedAt.IsZero() {
		result.CreatedAt = timestamppb.New(memory.CreatedAt)
	}
	if memory.ExpiresAt != nil {
		result.ExpiresAt = timestamppb.New(*memory.ExpiresAt)
	}
	return result
}
