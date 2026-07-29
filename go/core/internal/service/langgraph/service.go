package langgraph

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type Store interface {
	StoreCheckpoint(context.Context, *database.LangGraphCheckpoint) error
	StoreCheckpointWrites(context.Context, []*database.LangGraphCheckpointWrite) error
	ListCheckpoints(context.Context, string, string, string, *string, int) ([]*database.LangGraphCheckpointTuple, error)
	DeleteCheckpoint(context.Context, string, string) error
}

type Checkpoint struct {
	ThreadID           string
	CheckpointNS       string
	CheckpointID       string
	ParentCheckpointID *string
	Checkpoint         []byte
	Metadata           []byte
	Type               string
	Version            int64
}

type Write struct {
	Idx     int64
	Channel string
	Type    string
	Value   []byte
	TaskID  string
}

type Writes struct {
	ThreadID     string
	CheckpointNS string
	CheckpointID string
	TaskID       string
	Writes       []Write
}

type CheckpointTuple struct {
	Checkpoint *Checkpoint
	Writes     []Write
}

type ListRequest struct {
	ThreadID     string
	CheckpointNS string
	CheckpointID *string
	Limit        int
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) PutCheckpoint(ctx context.Context, checkpoint *Checkpoint) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if checkpoint == nil {
		return serviceerrors.NewInvalidArgument("checkpoint is required", nil)
	}
	if strings.TrimSpace(checkpoint.ThreadID) == "" {
		return serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if len(checkpoint.Checkpoint) == 0 {
		return serviceerrors.NewInvalidArgument("checkpoint is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to store checkpoint", fmt.Errorf("database client is not configured"))
	}

	value := &database.LangGraphCheckpoint{
		UserID:             userID,
		ThreadID:           checkpoint.ThreadID,
		CheckpointNS:       checkpoint.CheckpointNS,
		CheckpointID:       checkpoint.CheckpointID,
		ParentCheckpointID: checkpoint.ParentCheckpointID,
		Metadata:           base64.StdEncoding.EncodeToString(checkpoint.Metadata),
		Checkpoint:         base64.StdEncoding.EncodeToString(checkpoint.Checkpoint),
		CheckpointType:     checkpoint.Type,
		Version:            checkpoint.Version,
	}
	if err := s.store.StoreCheckpoint(ctx, value); err != nil {
		return serviceerrors.NewInternal("Failed to store checkpoint", err)
	}
	return nil
}

func (s *Service) ListCheckpoints(ctx context.Context, request ListRequest) ([]CheckpointTuple, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.ThreadID) == "" {
		return nil, serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to list checkpoints", fmt.Errorf("database client is not configured"))
	}
	if request.CheckpointID != nil && *request.CheckpointID == "" {
		request.CheckpointID = nil
	}
	if request.Limit < 0 {
		request.Limit = 0
	}

	values, err := s.store.ListCheckpoints(
		ctx,
		userID,
		request.ThreadID,
		request.CheckpointNS,
		request.CheckpointID,
		request.Limit,
	)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, serviceerrors.NewNotFound("Checkpoint not found", err)
		}
		return nil, serviceerrors.NewInternal("Failed to list checkpoints", err)
	}

	result := make([]CheckpointTuple, 0, len(values))
	for _, value := range values {
		if value == nil || value.Checkpoint == nil {
			return nil, serviceerrors.NewInternal("Failed to decode checkpoint", fmt.Errorf("database returned an empty checkpoint tuple"))
		}
		checkpoint, err := checkpointFromDatabase(value.Checkpoint)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to decode checkpoint", err)
		}
		writes := make([]Write, 0, len(value.Writes))
		for _, valueWrite := range value.Writes {
			write, err := writeFromDatabase(valueWrite)
			if err != nil {
				return nil, serviceerrors.NewInternal("Failed to decode checkpoint write", err)
			}
			writes = append(writes, write)
		}
		result = append(result, CheckpointTuple{Checkpoint: checkpoint, Writes: writes})
	}
	return result, nil
}

func (s *Service) PutWrites(ctx context.Context, writes *Writes) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if writes == nil {
		return serviceerrors.NewInvalidArgument("writes are required", nil)
	}
	if strings.TrimSpace(writes.ThreadID) == "" {
		return serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if strings.TrimSpace(writes.CheckpointID) == "" {
		return serviceerrors.NewInvalidArgument("checkpoint_id is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to store checkpoint writes", fmt.Errorf("database client is not configured"))
	}

	values := make([]*database.LangGraphCheckpointWrite, 0, len(writes.Writes))
	for _, write := range writes.Writes {
		values = append(values, &database.LangGraphCheckpointWrite{
			UserID:       userID,
			ThreadID:     writes.ThreadID,
			CheckpointNS: writes.CheckpointNS,
			CheckpointID: writes.CheckpointID,
			WriteIdx:     write.Idx,
			Value:        base64.StdEncoding.EncodeToString(write.Value),
			ValueType:    write.Type,
			Channel:      write.Channel,
			TaskID:       writes.TaskID,
		})
	}
	if err := s.store.StoreCheckpointWrites(ctx, values); err != nil {
		return serviceerrors.NewInternal("Failed to store checkpoint writes", err)
	}
	return nil
}

func (s *Service) DeleteThread(ctx context.Context, threadID string) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(threadID) == "" {
		return serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to delete thread", fmt.Errorf("database client is not configured"))
	}
	if err := s.store.DeleteCheckpoint(ctx, userID, threadID); err != nil {
		return serviceerrors.NewInternal("Failed to delete thread", err)
	}
	return nil
}

func checkpointFromDatabase(value *database.LangGraphCheckpoint) (*Checkpoint, error) {
	checkpoint, err := base64.StdEncoding.DecodeString(value.Checkpoint)
	if err != nil {
		return nil, fmt.Errorf("decode checkpoint %q: %w", value.CheckpointID, err)
	}
	metadata, err := base64.StdEncoding.DecodeString(value.Metadata)
	if err != nil {
		return nil, fmt.Errorf("decode checkpoint metadata %q: %w", value.CheckpointID, err)
	}
	return &Checkpoint{
		ThreadID:           value.ThreadID,
		CheckpointNS:       value.CheckpointNS,
		CheckpointID:       value.CheckpointID,
		ParentCheckpointID: value.ParentCheckpointID,
		Checkpoint:         checkpoint,
		Metadata:           metadata,
		Type:               value.CheckpointType,
		Version:            value.Version,
	}, nil
}

func writeFromDatabase(value *database.LangGraphCheckpointWrite) (Write, error) {
	if value == nil {
		return Write{}, fmt.Errorf("database returned an empty checkpoint write")
	}
	decoded, err := base64.StdEncoding.DecodeString(value.Value)
	if err != nil {
		return Write{}, fmt.Errorf("decode checkpoint write %d: %w", value.WriteIdx, err)
	}
	return Write{
		Idx:     value.WriteIdx,
		Channel: value.Channel,
		Type:    value.ValueType,
		Value:   decoded,
		TaskID:  value.TaskID,
	}, nil
}

func authenticatedUserID(ctx context.Context) (string, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return "", serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	userID := session.Principal().User.ID
	if userID == "" {
		return "", serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("user id is empty"))
	}
	return userID, nil
}
