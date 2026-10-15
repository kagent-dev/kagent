package grpcserver

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
)

type checkpointServer struct {
	apiv1.UnimplementedCheckpointServiceServer
	service *checkpoint.Service
}

func (s *checkpointServer) CreateCheckpoint(ctx context.Context, request *apiv1.CreateCheckpointRequest) (*apiv1.CreateCheckpointResponse, error) {
	checkpoint, err := s.service.Create(ctx, request.GetSessionId(), request.GetRequestId(), request.GetExpectedHeadTaskId())
	return &apiv1.CreateCheckpointResponse{Checkpoint: checkpoint}, err
}

func (s *checkpointServer) GetCheckpoint(ctx context.Context, request *apiv1.GetCheckpointRequest) (*apiv1.GetCheckpointResponse, error) {
	checkpoint, err := s.service.Get(ctx, request.GetCheckpointId())
	return &apiv1.GetCheckpointResponse{Checkpoint: checkpoint}, err
}

func (s *checkpointServer) ListCheckpoints(ctx context.Context, request *apiv1.ListCheckpointsRequest) (*apiv1.ListCheckpointsResponse, error) {
	page := request.GetPage()
	result, err := s.service.List(ctx, checkpoint.ListRequest{
		SessionID: request.GetSessionId(),
		PageSize:  int(page.GetLimit()), PageToken: page.GetPageToken(),
	})
	return &apiv1.ListCheckpointsResponse{
		Checkpoints: result.Checkpoints,
		Page:        &apiv1.PageResponse{NextPageToken: result.NextPageToken},
	}, err
}

func (s *checkpointServer) DeleteCheckpoint(ctx context.Context, request *apiv1.DeleteCheckpointRequest) (*apiv1.DeleteCheckpointResponse, error) {
	err := s.service.Delete(ctx, request.GetCheckpointId())
	return &apiv1.DeleteCheckpointResponse{}, err
}

func (s *checkpointServer) UpdateCheckpointName(ctx context.Context, request *apiv1.UpdateCheckpointNameRequest) (*apiv1.UpdateCheckpointNameResponse, error) {
	checkpoint, err := s.service.Rename(ctx, request.GetCheckpointId(), request.GetName())
	return &apiv1.UpdateCheckpointNameResponse{Checkpoint: checkpoint}, err
}

func (s *checkpointServer) ForkSession(ctx context.Context, request *apiv1.ForkSessionRequest) (*apiv1.ForkSessionResponse, error) {
	session, err := s.service.Fork(ctx, request.GetCheckpointId(), request.GetRequestId())
	return &apiv1.ForkSessionResponse{Session: session}, err
}
