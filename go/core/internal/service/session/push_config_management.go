package session

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"k8s.io/apimachinery/pkg/types"
)

// CreateTaskPushConfig registers or replaces a callback for an existing task.
// Task authorization precedes storage access so no configuration leaks across users.
func (s *InteractionService) CreateTaskPushConfig(ctx context.Context, agent types.NamespacedName, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	config, err := validatePushConfig(agent, req)
	if err != nil {
		return nil, err
	}
	if config.TaskID == "" {
		return nil, a2a.ErrInvalidParams
	}
	session, err := s.taskSession(ctx, agent, auth.VerbCreate, config.TaskID)
	if err != nil {
		return nil, err
	}
	if config.ID == "" {
		// Give this explicit Create a stable public ID before it reaches the store.
		config.ID = uuid.NewString()
	}
	if err := s.store.SaveTaskPushConfig(ctx, session.Id, string(config.TaskID), config); err != nil {
		return nil, pushStoreError(ctx, err)
	}
	return publicPushConfig(config), nil
}

func (s *InteractionService) GetTaskPushConfig(ctx context.Context, agent types.NamespacedName, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	if req == nil || req.TaskID == "" || req.ID == "" {
		return nil, a2a.ErrInvalidParams
	}
	session, err := s.taskSession(ctx, agent, auth.VerbGet, req.TaskID)
	if err != nil {
		return nil, err
	}
	config, err := s.store.GetTaskPushConfig(ctx, session.Id, string(req.TaskID), req.ID)
	if err != nil {
		return nil, pushStoreError(ctx, err)
	}
	return &a2a.PushConfig{TaskID: req.TaskID, ID: config.ID, URL: config.URL}, nil
}

func (s *InteractionService) ListTaskPushConfigs(ctx context.Context, agent types.NamespacedName, req *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	if req == nil || req.TaskID == "" {
		return nil, a2a.ErrInvalidParams
	}
	session, err := s.taskSession(ctx, agent, auth.VerbGet, req.TaskID)
	if err != nil {
		return nil, err
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "page size must be between 1 and 100")
	}
	afterID, err := decodePushPageToken(string(req.TaskID), req.PageToken)
	if err != nil {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "invalid page token")
	}
	// Fetch one extra row to decide whether the client needs another page.
	rows, err := s.store.ListTaskPushConfigs(ctx, session.Id, string(req.TaskID), afterID, pageSize+1)
	if err != nil {
		return nil, pushStoreError(ctx, err)
	}
	response := &a2a.ListTaskPushConfigResponse{Configs: make([]*a2a.PushConfig, 0, min(len(rows), pageSize))}
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		response.NextPageToken = encodePushPageToken(string(req.TaskID), rows[len(rows)-1].ID)
	}
	for _, row := range rows {
		response.Configs = append(response.Configs, &a2a.PushConfig{TaskID: req.TaskID, ID: row.ID, URL: row.URL})
	}
	return response, nil
}

// Read and create responses identify the callback without disclosing either secret.
func publicPushConfig(config *a2a.PushConfig) *a2a.PushConfig {
	return &a2a.PushConfig{TaskID: config.TaskID, ID: config.ID, URL: config.URL, Tenant: config.Tenant}
}

func encodePushPageToken(taskID, configID string) string {
	// A token from one task must not advance another task's listing.
	return base64.RawURLEncoding.EncodeToString([]byte(taskID + "\x00" + configID))
}

func decodePushPageToken(taskID, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", err
	}
	owner, configID, found := strings.Cut(string(data), "\x00")
	if !found || owner != taskID || configID == "" {
		return "", a2a.ErrInvalidParams
	}
	return configID, nil
}

func (s *InteractionService) DeleteTaskPushConfig(ctx context.Context, agent types.NamespacedName, req *a2a.DeleteTaskPushConfigRequest) error {
	if req == nil || req.TaskID == "" || req.ID == "" {
		return a2a.ErrInvalidParams
	}
	session, err := s.taskSession(ctx, agent, auth.VerbDelete, req.TaskID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteTaskPushConfig(ctx, session.Id, string(req.TaskID), req.ID); err != nil {
		return pushStoreError(ctx, err)
	}
	return nil
}

func pushStoreError(ctx context.Context, err error) error {
	if errors.Is(err, database.ErrNotFound) {
		return a2a.ErrTaskNotFound
	}
	if errors.Is(err, database.ErrFailedPrecondition) {
		return a2a.NewError(a2a.ErrInvalidRequest, "cannot register a callback on a terminal task")
	}
	logging.FromContext(ctx).ErrorContext(ctx, "failed to manage task push configuration", "error", err)
	return a2a.NewError(a2a.ErrInternalError, "failed to manage task push configuration")
}
