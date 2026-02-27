package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Session defines the session operations
type Session interface {
	ListSessions(ctx context.Context) (*api.StandardResponse[[]*api.Session], error)
	CreateSession(ctx context.Context, request *api.SessionRequest) (*api.StandardResponse[*api.Session], error)
	GetSession(ctx context.Context, sessionName string) (*api.StandardResponse[*api.Session], error)
	UpdateSession(ctx context.Context, request *api.SessionRequest) (*api.StandardResponse[*api.Session], error)
	DeleteSession(ctx context.Context, sessionName string) error
	ListSessionRuns(ctx context.Context, sessionName string) (*api.StandardResponse[any], error)
}

// sessionClient handles session-related requests
type sessionClient struct {
	client *BaseClient
}

// NewSessionClient creates a new session client
func NewSessionClient(client *BaseClient) Session {
	return &sessionClient{client: client}
}

// ListSessions lists all sessions for a user
func (c *sessionClient) ListSessions(ctx context.Context) (*api.StandardResponse[[]*api.Session], error) {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	resp, err := c.client.Get(ctx, "/api/sessions", userID)
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[[]*api.Session]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// CreateSession creates a new session
func (c *sessionClient) CreateSession(ctx context.Context, request *api.SessionRequest) (*api.StandardResponse[*api.Session], error) {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}
	resp, err := c.client.Post(ctx, "/api/sessions", request, userID)
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*api.Session]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// GetSession retrieves a specific session
func (c *sessionClient) GetSession(ctx context.Context, sessionName string) (*api.StandardResponse[*api.Session], error) {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	path := fmt.Sprintf("/api/sessions/%s", sessionName)
	resp, err := c.client.Get(ctx, path, userID)
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*api.Session]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// UpdateSession updates an existing session
func (c *sessionClient) UpdateSession(ctx context.Context, request *api.SessionRequest) (*api.StandardResponse[*api.Session], error) {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	resp, err := c.client.Put(ctx, "/api/sessions", request, userID)
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[*api.Session]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// DeleteSession deletes a session
func (c *sessionClient) DeleteSession(ctx context.Context, sessionName string) error {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return fmt.Errorf("userID is required")
	}

	path := fmt.Sprintf("/api/sessions/%s", sessionName)
	_, err := c.client.Delete(ctx, path, userID)
	if err != nil {
		return err
	}
	return nil
}

// ListSessionRuns lists all runs for a specific session
func (c *sessionClient) ListSessionRuns(ctx context.Context, sessionName string) (*api.StandardResponse[any], error) {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}

	path := fmt.Sprintf("/api/sessions/%s/runs", sessionName)
	resp, err := c.client.Get(ctx, path, userID)
	if err != nil {
		return nil, err
	}

	var response api.StandardResponse[any]
	if err := DecodeResponse(resp, &response); err != nil {
		return nil, err
	}

	return &response, nil
}
