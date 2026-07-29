package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	langgraphservice "github.com/kagent-dev/kagent/go/core/internal/service/langgraph"
)

type langGraphServer struct {
	apiv1alpha1.UnimplementedLangGraphServiceServer
	service *langgraphservice.Service
}

func newLangGraphServer(service *langgraphservice.Service) *langGraphServer {
	return &langGraphServer{service: service}
}

func (s *langGraphServer) PutCheckpoint(ctx context.Context, request *apiv1alpha1.PutCheckpointRequest) (*apiv1alpha1.PutCheckpointResponse, error) {
	checkpoint := request.GetCheckpoint()
	if err := s.service.PutCheckpoint(ctx, checkpointFromProto(checkpoint)); err != nil {
		return nil, err
	}
	return &apiv1alpha1.PutCheckpointResponse{}, nil
}

func (s *langGraphServer) ListCheckpoints(ctx context.Context, request *apiv1alpha1.ListCheckpointsRequest) (*apiv1alpha1.ListCheckpointsResponse, error) {
	limit := 0
	if request.Limit != nil {
		limit = int(request.GetLimit())
	}
	values, err := s.service.ListCheckpoints(ctx, langgraphservice.ListRequest{
		ThreadID:     request.GetThreadId(),
		CheckpointNS: request.GetCheckpointNs(),
		CheckpointID: request.CheckpointId,
		Limit:        limit,
	})
	if err != nil {
		return nil, err
	}

	checkpoints := make([]*apiv1alpha1.LangGraphCheckpointTuple, 0, len(values))
	for _, value := range values {
		checkpoints = append(checkpoints, checkpointTupleToProto(value))
	}
	return &apiv1alpha1.ListCheckpointsResponse{Checkpoints: checkpoints}, nil
}

func (s *langGraphServer) PutWrites(ctx context.Context, request *apiv1alpha1.PutWritesRequest) (*apiv1alpha1.PutWritesResponse, error) {
	if err := s.service.PutWrites(ctx, writesFromProto(request.GetWrites())); err != nil {
		return nil, err
	}
	return &apiv1alpha1.PutWritesResponse{}, nil
}

func (s *langGraphServer) DeleteThread(ctx context.Context, request *apiv1alpha1.DeleteThreadRequest) (*apiv1alpha1.DeleteThreadResponse, error) {
	if err := s.service.DeleteThread(ctx, request.GetThreadId()); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteThreadResponse{}, nil
}

func checkpointFromProto(value *apiv1alpha1.LangGraphCheckpoint) *langgraphservice.Checkpoint {
	if value == nil {
		return nil
	}
	return &langgraphservice.Checkpoint{
		ThreadID:           value.GetThreadId(),
		CheckpointNS:       value.GetCheckpointNs(),
		CheckpointID:       value.GetCheckpointId(),
		ParentCheckpointID: value.ParentCheckpointId,
		Checkpoint:         value.GetCheckpoint(),
		Metadata:           value.GetMetadata(),
		Type:               value.GetType(),
		Version:            value.GetVersion(),
	}
}

func writesFromProto(value *apiv1alpha1.LangGraphCheckpointWrites) *langgraphservice.Writes {
	if value == nil {
		return nil
	}
	writes := make([]langgraphservice.Write, 0, len(value.GetWrites()))
	for _, write := range value.GetWrites() {
		writes = append(writes, langgraphservice.Write{
			Idx:     write.GetIdx(),
			Channel: write.GetChannel(),
			Type:    write.GetType(),
			Value:   write.GetValue(),
		})
	}
	return &langgraphservice.Writes{
		ThreadID:     value.GetThreadId(),
		CheckpointNS: value.GetCheckpointNs(),
		CheckpointID: value.GetCheckpointId(),
		TaskID:       value.GetTaskId(),
		Writes:       writes,
	}
}

func checkpointTupleToProto(value langgraphservice.CheckpointTuple) *apiv1alpha1.LangGraphCheckpointTuple {
	checkpoint := checkpointToProto(value.Checkpoint)
	writes := make([]*apiv1alpha1.LangGraphCheckpointWrite, 0, len(value.Writes))
	taskID := ""
	for _, write := range value.Writes {
		taskID = write.TaskID
		writes = append(writes, &apiv1alpha1.LangGraphCheckpointWrite{
			Idx:     write.Idx,
			Channel: write.Channel,
			Type:    write.Type,
			Value:   write.Value,
			TaskId:  write.TaskID,
		})
	}
	return &apiv1alpha1.LangGraphCheckpointTuple{
		Checkpoint: checkpoint,
		Writes: &apiv1alpha1.LangGraphCheckpointWrites{
			ThreadId:     checkpoint.GetThreadId(),
			CheckpointNs: checkpoint.GetCheckpointNs(),
			CheckpointId: checkpoint.GetCheckpointId(),
			TaskId:       taskID,
			Writes:       writes,
		},
	}
}

func checkpointToProto(value *langgraphservice.Checkpoint) *apiv1alpha1.LangGraphCheckpoint {
	if value == nil {
		return nil
	}
	return &apiv1alpha1.LangGraphCheckpoint{
		ThreadId:           value.ThreadID,
		CheckpointNs:       value.CheckpointNS,
		CheckpointId:       value.CheckpointID,
		ParentCheckpointId: value.ParentCheckpointID,
		Checkpoint:         value.Checkpoint,
		Metadata:           value.Metadata,
		Type:               value.Type,
		Version:            value.Version,
	}
}
