package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type Store interface {
	StoreTask(context.Context, *a2a.Task, string) error
	GetTask(context.Context, string, string) (*a2a.Task, error)
	DeleteTask(context.Context, string, string) error
	GetSession(context.Context, string, string) (*database.Session, error)
	ListTasksForSession(context.Context, string, string) ([]*a2a.Task, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, task *a2a.Task) (*a2a.Task, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, serviceerrors.NewInvalidArgument("task is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to create task", fmt.Errorf("database client is not configured"))
	}
	if task.ID == "" {
		task.ID = a2a.NewTaskID()
	}
	if err := s.store.StoreTask(ctx, task, userID); err != nil {
		if errors.Is(err, database.ErrTaskOwnedByAnotherUser) {
			return nil, serviceerrors.NewAlreadyExists("Task ID is already in use", err)
		}
		return nil, serviceerrors.NewInternal("Failed to create task", err)
	}
	return task, nil
}

func (s *Service) Get(ctx context.Context, taskID string) (*a2a.Task, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, serviceerrors.NewInvalidArgument("task_id is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to get task", fmt.Errorf("database client is not configured"))
	}
	task, err := s.store.GetTask(ctx, taskID, userID)
	if err != nil {
		return nil, mapStoreError("Task not found", err)
	}
	return task, nil
}

func (s *Service) Delete(ctx context.Context, taskID string) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(taskID) == "" {
		return serviceerrors.NewInvalidArgument("task_id is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to delete task", fmt.Errorf("database client is not configured"))
	}
	if err := s.store.DeleteTask(ctx, taskID, userID); err != nil {
		if errors.Is(err, database.ErrTaskOwnedByAnotherUser) || errors.Is(err, database.ErrNotFound) {
			return serviceerrors.NewNotFound("Task not found", err)
		}
		return serviceerrors.NewInternal("Failed to delete task", err)
	}
	return nil
}

func (s *Service) List(ctx context.Context, sessionID string) ([]*a2a.Task, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, serviceerrors.NewInvalidArgument("session_id is required", nil)
	}
	userID, err := effectiveUserID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to get session tasks", fmt.Errorf("database client is not configured"))
	}
	if _, err := s.store.GetSession(ctx, sessionID, userID); err != nil {
		return nil, mapStoreError("Session not found for given ID", err)
	}
	tasks, err := s.store.ListTasksForSession(ctx, sessionID, userID)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get session runs", err)
	}
	return tasks, nil
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

func effectiveUserID(ctx context.Context, sessionID string) (string, error) {
	if share, ok := auth.ShareContextFrom(ctx); ok && share.SessionID == sessionID {
		return share.UserID, nil
	}
	return authenticatedUserID(ctx)
}

func mapStoreError(message string, err error) error {
	if errors.Is(err, database.ErrNotFound) {
		return serviceerrors.NewNotFound(message, err)
	}
	return serviceerrors.NewInternal(message, err)
}
