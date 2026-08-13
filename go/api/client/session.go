package client

import (
	"context"
	"fmt"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
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
	userID, err := c.userID()
	if err != nil {
		return nil, err
	}
	client, err := c.client.sessionServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	response, err := client.ListSessions(callContext, &apiv1alpha1.ListSessionsRequest{})
	if err != nil {
		return nil, err
	}
	sessions := make([]*api.Session, 0, len(response.GetSessions()))
	for _, value := range response.GetSessions() {
		sessions = append(sessions, sessionFromProto(value))
	}
	result := api.NewResponse(sessions, "Successfully listed sessions", false)
	return &result, nil
}

// CreateSession creates a new session
func (c *sessionClient) CreateSession(ctx context.Context, request *api.SessionRequest) (*api.StandardResponse[*api.Session], error) {
	if request == nil {
		return nil, fmt.Errorf("session request is required")
	}
	userID, err := c.userID()
	if err != nil {
		return nil, err
	}
	client, err := c.client.sessionServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	grpcRequest := &apiv1alpha1.CreateSessionRequest{
		Id:       request.ID,
		Name:     request.Name,
		AgentRef: dereference(request.AgentRef),
	}
	if request.Source != nil {
		source, conversionErr := sessionSourceToProto(*request.Source)
		if conversionErr != nil {
			return nil, conversionErr
		}
		grpcRequest.Source = &source
	}
	response, err := client.CreateSession(callContext, grpcRequest)
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(sessionFromProto(response.GetSession()), "Successfully created session", false)
	return &result, nil
}

// GetSession retrieves a specific session
func (c *sessionClient) GetSession(ctx context.Context, sessionName string) (*api.StandardResponse[*api.Session], error) {
	userID, err := c.userID()
	if err != nil {
		return nil, err
	}
	client, err := c.client.sessionServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	response, err := client.GetSession(callContext, &apiv1alpha1.GetSessionRequest{SessionId: sessionName})
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(sessionFromProto(response.GetSession()), "Successfully retrieved session", false)
	return &result, nil
}

// UpdateSession updates an existing session
func (c *sessionClient) UpdateSession(ctx context.Context, request *api.SessionRequest) (*api.StandardResponse[*api.Session], error) {
	if request == nil {
		return nil, fmt.Errorf("session request is required")
	}
	if request.ID == nil || *request.ID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	userID, err := c.userID()
	if err != nil {
		return nil, err
	}
	client, err := c.client.sessionServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	response, err := client.UpdateSession(callContext, &apiv1alpha1.UpdateSessionRequest{
		SessionId: *request.ID,
		Name:      request.Name,
		AgentRef:  request.AgentRef,
	})
	if err != nil {
		return nil, err
	}
	result := api.NewResponse(sessionFromProto(response.GetSession()), "Successfully updated session", false)
	return &result, nil
}

// DeleteSession deletes a session
func (c *sessionClient) DeleteSession(ctx context.Context, sessionName string) error {
	userID, err := c.userID()
	if err != nil {
		return err
	}
	client, err := c.client.sessionServiceClient()
	if err != nil {
		return err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	_, err = client.DeleteSession(callContext, &apiv1alpha1.DeleteSessionRequest{SessionId: sessionName})
	return err
}

// ListSessionRuns lists all runs for a specific session
func (c *sessionClient) ListSessionRuns(ctx context.Context, sessionName string) (*api.StandardResponse[any], error) {
	userID, err := c.userID()
	if err != nil {
		return nil, err
	}
	client, err := c.client.taskServiceClient()
	if err != nil {
		return nil, err
	}
	callContext, cancel := c.client.grpcCallContextForUser(ctx, userID)
	defer cancel()
	response, err := client.ListTasks(callContext, &apiv1alpha1.ListTasksRequest{SessionId: sessionName})
	if err != nil {
		return nil, err
	}
	tasks := make([]*a2a.Task, 0, len(response.GetTasks()))
	for _, value := range response.GetTasks() {
		task, err := pbconv.FromProtoTask(value)
		if err != nil {
			return nil, fmt.Errorf("decode task: %w", err)
		}
		tasks = append(tasks, task)
	}
	result := api.NewResponse[any](tasks, "Successfully retrieved session tasks", false)
	return &result, nil
}

func (c *sessionClient) userID() (string, error) {
	userID := c.client.GetUserIDOrDefault("")
	if userID == "" {
		return "", fmt.Errorf("userID is required")
	}
	return userID, nil
}

func (c *BaseClient) sessionServiceClient() (apiv1alpha1.SessionServiceClient, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewSessionServiceClient(connection), nil
}

func (c *BaseClient) taskServiceClient() (apiv1alpha1.TaskStoreServiceClient, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewTaskStoreServiceClient(connection), nil
}

func sessionFromProto(value *apiv1alpha1.Session) *api.Session {
	if value == nil {
		return nil
	}
	result := &api.Session{
		ID:      value.GetId(),
		Name:    value.Name,
		UserID:  value.GetUserId(),
		AgentID: value.AgentId,
	}
	if value.GetCreatedAt() != nil {
		result.CreatedAt = value.GetCreatedAt().AsTime()
	}
	if value.GetUpdatedAt() != nil {
		result.UpdatedAt = value.GetUpdatedAt().AsTime()
	}
	if value.GetDeletedAt() != nil {
		result.DeletedAt = new(value.GetDeletedAt().AsTime())
	}
	if value.Source != nil {
		result.Source = sessionSourceFromProto(*value.Source)
	}
	return result
}

func sessionSourceToProto(value database.SessionSource) (apiv1alpha1.SessionSource, error) {
	switch value {
	case database.SessionSourceUser:
		return apiv1alpha1.SessionSource_SESSION_SOURCE_USER, nil
	case database.SessionSourceAgent:
		return apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT, nil
	default:
		return apiv1alpha1.SessionSource_SESSION_SOURCE_UNSPECIFIED, fmt.Errorf("unsupported session source %q", value)
	}
}

func sessionSourceFromProto(value apiv1alpha1.SessionSource) *database.SessionSource {
	var source database.SessionSource
	switch value {
	case apiv1alpha1.SessionSource_SESSION_SOURCE_USER:
		source = database.SessionSourceUser
	case apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT:
		source = database.SessionSourceAgent
	default:
		return nil
	}
	return &source
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
