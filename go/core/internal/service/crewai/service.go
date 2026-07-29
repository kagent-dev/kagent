package crewai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type Store interface {
	StoreCrewAIMemory(context.Context, *database.CrewAIAgentMemory) error
	SearchCrewAIMemoryByTask(context.Context, string, string, string, int) ([]*database.CrewAIAgentMemory, error)
	ResetCrewAIMemory(context.Context, string, string) error
	StoreCrewAIFlowState(context.Context, *database.CrewAIFlowState) error
	GetCrewAIFlowState(context.Context, string, string) (*database.CrewAIFlowState, error)
}

type Memory struct {
	ThreadID string
	UserID   string
	Data     map[string]any
}

type FlowState struct {
	ThreadID   string
	MethodName string
	Data       map[string]any
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) StoreMemory(ctx context.Context, threadID string, data map[string]any) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(threadID) == "" {
		return serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to store CrewAI memory", fmt.Errorf("database client is not configured"))
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return serviceerrors.NewInvalidArgument("Failed to serialize memory data", err)
	}
	if err := s.store.StoreCrewAIMemory(ctx, &database.CrewAIAgentMemory{
		UserID:     userID,
		ThreadID:   threadID,
		MemoryData: string(encoded),
	}); err != nil {
		return serviceerrors.NewInternal("Failed to store CrewAI memory", err)
	}
	return nil
}

func (s *Service) GetMemory(ctx context.Context, threadID, taskDescription string, limit int) ([]Memory, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(threadID) == "" {
		return nil, serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if strings.TrimSpace(taskDescription) == "" {
		return nil, serviceerrors.NewInvalidArgument("task_description is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to list CrewAI memory", fmt.Errorf("database client is not configured"))
	}
	if limit < 0 {
		limit = 0
	}

	values, err := s.store.SearchCrewAIMemoryByTask(ctx, userID, threadID, taskDescription, limit)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to list CrewAI memory", err)
	}
	result := make([]Memory, 0, len(values))
	for _, value := range values {
		if value == nil {
			return nil, serviceerrors.NewInternal("Failed to parse memory data", fmt.Errorf("database returned an empty CrewAI memory"))
		}
		data := map[string]any{}
		if err := json.Unmarshal([]byte(value.MemoryData), &data); err != nil {
			return nil, serviceerrors.NewInternal("Failed to parse memory data", err)
		}
		result = append(result, Memory{ThreadID: value.ThreadID, UserID: value.UserID, Data: data})
	}
	return result, nil
}

func (s *Service) ResetMemory(ctx context.Context, threadID string) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(threadID) == "" {
		return serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to reset CrewAI memory", fmt.Errorf("database client is not configured"))
	}
	if err := s.store.ResetCrewAIMemory(ctx, userID, threadID); err != nil {
		return serviceerrors.NewInternal("Failed to reset CrewAI memory", err)
	}
	return nil
}

func (s *Service) StoreFlowState(ctx context.Context, state *FlowState) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if state == nil {
		return serviceerrors.NewInvalidArgument("flow state is required", nil)
	}
	if strings.TrimSpace(state.ThreadID) == "" {
		return serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if strings.TrimSpace(state.MethodName) == "" {
		return serviceerrors.NewInvalidArgument("method_name is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to store CrewAI flow state", fmt.Errorf("database client is not configured"))
	}

	encoded, err := json.Marshal(state.Data)
	if err != nil {
		return serviceerrors.NewInvalidArgument("Failed to serialize state data", err)
	}
	if err := s.store.StoreCrewAIFlowState(ctx, &database.CrewAIFlowState{
		UserID:     userID,
		ThreadID:   state.ThreadID,
		MethodName: state.MethodName,
		StateData:  string(encoded),
	}); err != nil {
		return serviceerrors.NewInternal("Failed to store CrewAI flow state", err)
	}
	return nil
}

func (s *Service) GetFlowState(ctx context.Context, threadID string) (*FlowState, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(threadID) == "" {
		return nil, serviceerrors.NewInvalidArgument("thread_id is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to get CrewAI flow state", fmt.Errorf("database client is not configured"))
	}

	value, err := s.store.GetCrewAIFlowState(ctx, userID, threadID)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get CrewAI flow state", err)
	}
	if value == nil {
		return nil, serviceerrors.NewNotFound("Flow state not found", nil)
	}
	data := map[string]any{}
	if err := json.Unmarshal([]byte(value.StateData), &data); err != nil {
		return nil, serviceerrors.NewInternal("Failed to parse state data", err)
	}
	return &FlowState{ThreadID: value.ThreadID, MethodName: value.MethodName, Data: data}, nil
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
