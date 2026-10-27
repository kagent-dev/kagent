package memory

import (
	"context"
	"errors"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// DeleteEntry removes one memory after resolving the caller's agent and user scope.
func (s *Service) DeleteEntry(ctx context.Context, agentName, id string) error {
	agentName, userID, err := s.managementScope(ctx, auth.VerbDelete, agentName)
	if err != nil {
		return err
	}
	if agentName == "" || userID == "" || id == "" {
		return serviceerrors.NewInvalidArgument("agent and memory ID are required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("memory store is not configured", nil)
	}
	err = s.store.DeleteMemoryEntry(ctx, agentName, userID, id)
	if errors.Is(err, database.ErrNotFound) {
		return serviceerrors.NewNotFound("memory entry not found", err)
	}
	if err != nil {
		return serviceerrors.NewInternal("failed to delete memory entry", err)
	}
	return nil
}
