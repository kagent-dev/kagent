package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type Client interface {
	CreateFeedback(feedback *FeedbackSubmission) error
	CreateRun(req *CreateRunRequest) (*CreateRunResult, error)
	CreateSession(session *CreateSession) (*Session, error)
	CreateTeam(team *Team) error
	CreateToolServer(toolServer *ToolServer, userID string) (*ToolServer, error)
	DeleteRun(runID uuid.UUID) error
	DeleteSession(sessionID int, userID string) error
	DeleteTeam(teamID int, userID string) error
	DeleteToolServer(serverID *int, userID string) error
	GetRun(runID int) (*Run, error)
	GetRunMessages(runID uuid.UUID) ([]*RunMessage, error)
	GetSession(sessionLabel string, userID string) (*Session, error)
	GetSessionById(sessionID int, userID string) (*Session, error)
	GetTeam(teamLabel string, userID string) (*Team, error)
	GetTeamByID(teamID int, userID string) (*Team, error)
	GetTool(provider string, userID string) (*Tool, error)
	GetToolServer(serverID int, userID string) (*ToolServer, error)
	GetToolServerByLabel(toolServerLabel string, userID string) (*ToolServer, error)
	GetVersion(ctx context.Context) (string, error)
	InvokeSession(sessionID int, userID string, request *InvokeRequest) (*TeamResult, error)
	InvokeSessionStream(sessionID int, userID string, request *InvokeRequest) (<-chan *SseEvent, error)
	InvokeTask(req *InvokeTaskRequest) (*InvokeTaskResult, error)
	InvokeTaskStream(req *InvokeTaskRequest) (<-chan *SseEvent, error)
	ListFeedback(userID string) ([]*FeedbackSubmission, error)
	ListRuns(userID string) ([]*Run, error)
	ListSessionRuns(sessionID int, userID string) ([]*Run, error)
	ListSessions(userID string) ([]*Session, error)
	ListSupportedModels() (*ProviderModels, error)
	ListTeams(userID string) ([]*Team, error)
	ListToolServers(userID string) ([]*ToolServer, error)
	ListTools(userID string) ([]*Tool, error)
	ListToolsForServer(serverID *int, userID string) ([]*Tool, error)
	RefreshToolServer(serverID int, userID string) error
	RefreshTools(serverID *int, userID string) error
	UpdateSession(sessionID int, userID string, session *Session) (*Session, error)
	UpdateToolServer(server *ToolServer, userID string) error
	Validate(req *ValidationRequest) (*ValidationResponse, error)
}

func New(baseURL string) Client {
	// Ensure baseURL doesn't end with a slash
	baseURL = strings.TrimRight(baseURL, "/")

	return &client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: time.Minute * 30,
		},
	}
}

func (c *client) GetVersion(ctx context.Context) (string, error) {
	var result struct {
		Version string `json:"version"`
	}

	err := c.doRequest(context.Background(), "GET", "/version", nil, &result)
	if err != nil {
		return "", err
	}

	return result.Version, nil
}

func (c *client) startRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("error marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Ensure path starts with a slash
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	url := c.BaseURL + path

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequestWithContext(ctx, method, url, bodyReader)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return c.HTTPClient.Do(req)
}

func (c *client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	resp, err := c.startRequest(ctx, method, path, body)
	if err != nil {
		return fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("request failed with status: %s", resp.Status)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	// Try decoding into APIResponse first
	var apiResp APIResponse

	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&apiResp); err != nil {
		// Trying the base value
		return json.Unmarshal(b, result)
	} else {
		// Check response status
		if !apiResp.Status {
			return fmt.Errorf("api error: [%+v]", apiResp)
		}

		// If caller wants the result, marshal the Data field into their result type
		if result != nil {
			dataBytes, err := json.Marshal(apiResp.Data)
			if err != nil {
				return fmt.Errorf("error re-marshaling data: %w", err)
			}

			if err := json.Unmarshal(dataBytes, result); err != nil {
				return fmt.Errorf("error unmarshaling into result: %w", err)
			}
		}
	}

	return nil
}
