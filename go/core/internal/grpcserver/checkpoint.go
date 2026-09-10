package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/checkpoint"
)

type checkpointServer struct {
	apiv1alpha1.UnimplementedCheckpointServiceServer
	service *checkpoint.Service
}

func (s *checkpointServer) CreateCheckpoint(ctx context.Context, request *apiv1alpha1.CreateCheckpointRequest) (*apiv1alpha1.CreateCheckpointResponse, error) {
	checkpoint, err := s.service.Create(ctx, request.GetAgentInstanceId(), request.GetRequestId())
	return &apiv1alpha1.CreateCheckpointResponse{Checkpoint: checkpoint}, err
}

func (s *checkpointServer) GetCheckpoint(ctx context.Context, request *apiv1alpha1.GetCheckpointRequest) (*apiv1alpha1.GetCheckpointResponse, error) {
	checkpoint, err := s.service.Get(ctx, request.GetCheckpointId())
	return &apiv1alpha1.GetCheckpointResponse{Checkpoint: checkpoint}, err
}

func (s *checkpointServer) ListCheckpoints(ctx context.Context, request *apiv1alpha1.ListCheckpointsRequest) (*apiv1alpha1.ListCheckpointsResponse, error) {
	page := request.GetPage()
	result, err := s.service.List(ctx, checkpoint.ListRequest{
		InstanceID: request.GetAgentInstanceId(),
		PageSize:   int(page.GetLimit()), PageToken: page.GetPageToken(),
		Offset: int(page.GetOffset()),
		Filter: request.GetFilter(),
		// An unknown enum member sorts by nothing, rather than by a guess.
		SortField:  checkpointSortColumns[request.GetSortBy().GetField()],
		Descending: request.GetSortBy().GetDirection() == apiv1alpha1.SortDirection_SORT_DIRECTION_DESC,
	})
	return &apiv1alpha1.ListCheckpointsResponse{
		Checkpoints: result.Checkpoints,
		Page:        &apiv1alpha1.PageResponse{NextPageToken: result.NextPageToken},
		TotalSize:   int32(result.TotalSize),
	}, err
}

var checkpointSortColumns = map[apiv1alpha1.CheckpointSortField]string{
	apiv1alpha1.CheckpointSortField_CHECKPOINT_SORT_FIELD_CREATED_AT:   "created_at",
	apiv1alpha1.CheckpointSortField_CHECKPOINT_SORT_FIELD_CONVERSATION: "conversation",
}

func (s *checkpointServer) DeleteCheckpoint(ctx context.Context, request *apiv1alpha1.DeleteCheckpointRequest) (*apiv1alpha1.DeleteCheckpointResponse, error) {
	err := s.service.Delete(ctx, request.GetCheckpointId())
	return &apiv1alpha1.DeleteCheckpointResponse{}, err
}

func (s *checkpointServer) ForkAgentInstance(ctx context.Context, request *apiv1alpha1.ForkAgentInstanceRequest) (*apiv1alpha1.ForkAgentInstanceResponse, error) {
	instance, err := s.service.Fork(ctx, request.GetCheckpointId(), request.GetRequestId())
	return &apiv1alpha1.ForkAgentInstanceResponse{AgentInstance: instance}, err
}
