package grpcserver

import (
	"context"

	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

func (s *memoryServer) DeleteEntry(ctx context.Context, request *runtimev1.MemoryServiceDeleteEntryRequest) (*runtimev1.MemoryServiceDeleteEntryResponse, error) {
	if err := s.service.DeleteEntry(ctx, request.GetAgentName(), request.GetId()); err != nil {
		return nil, err
	}
	return &runtimev1.MemoryServiceDeleteEntryResponse{}, nil
}
