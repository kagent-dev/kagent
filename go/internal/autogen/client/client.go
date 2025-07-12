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

	"github.com/kagent-dev/kagent/go/internal/autogen/api"
)

type client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type Client interface {
	GetVersion(ctx context.Context) (string, error)
	InvokeTask(ctx context.Context, req *InvokeTaskRequest) (*InvokeTaskResult, error)
	InvokeTaskStream(ctx context.Context, req *InvokeTaskRequest) (<-chan *SseEvent, error)
	FetchTools(ctx context.Context, req *ToolServerRequest) (*ToolServerResponse, error)
	Validate(ctx context.Context, req *ValidationRequest) (*ValidationResponse, error)
	ListSupportedModels(ctx context.Context) (*ProviderModels, error)
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

type InvokeTaskRequest struct {
	Task       string         `json:"task"`
	TeamConfig *api.Component `json:"team_config"`
	Messages   []Event        `json:"messages"`
}

type InvokeTaskResult struct {
	Duration   float64    `json:"duration"`
	TaskResult TaskResult `json:"task_result"`
	Usage      string     `json:"usage"`
}

func (c *client) InvokeTask(ctx context.Context, req *InvokeTaskRequest) (*InvokeTaskResult, error) {
	var invoke InvokeTaskResult
	{
		bytes, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("error marshaling request: %w", err)
		}
		fmt.Println(string(bytes))
	}

	err := c.doRequest(ctx, "POST", "/invoke", req, &invoke)
	return &invoke, err
}

func (c *client) InvokeTaskStream(ctx context.Context, req *InvokeTaskRequest) (<-chan *SseEvent, error) {
	resp, err := c.startRequest(ctx, "POST", "/invoke/stream", req)
	if err != nil {
		return nil, err
	}
	ch := streamSseResponse(resp.Body)
	return ch, nil
}

type ToolServerRequest struct {
	Server *api.Component `json:"server"`
}

type NamedTool struct {
	Name      string         `json:"name"`
	Component *api.Component `json:"component"`
}

type ToolServerResponse struct {
	Tools []*NamedTool `json:"tools"`
}

func (c *client) FetchTools(ctx context.Context, req *ToolServerRequest) (*ToolServerResponse, error) {
	var tools ToolServerResponse
	err := c.doRequest(ctx, "POST", "/toolservers", req, &tools)
	if err != nil {
		return nil, err
	}

	return &tools, err
}

type ValidationRequest struct {
	Component *api.Component `json:"component"`
}

type ValidationError struct {
	Field      string  `json:"field"`
	Error      string  `json:"error"`
	Suggestion *string `json:"suggestion,omitempty"`
}

type ValidationResponse struct {
	IsValid  bool               `json:"is_valid"`
	Errors   []*ValidationError `json:"errors"`
	Warnings []*ValidationError `json:"warnings"`
}

func (r ValidationResponse) ErrorMsg() string {
	var msg string
	for _, e := range r.Errors {
		msg += fmt.Sprintf("Error: %s\n [%s]\n", e.Error, e.Field)
		if e.Suggestion != nil {
			msg += fmt.Sprintf("Suggestion: %s\n", *e.Suggestion)
		}
	}
	for _, w := range r.Warnings {
		msg += fmt.Sprintf("Warning: %s\n [%s]\n", w.Error, w.Field)
		if w.Suggestion != nil {
			msg += fmt.Sprintf("Suggestion: %s\n", *w.Suggestion)
		}
	}

	return msg
}

func (c *client) Validate(ctx context.Context, req *ValidationRequest) (*ValidationResponse, error) {
	var resp ValidationResponse
	err := c.doRequest(ctx, "POST", "/validate", req, &resp)
	return &resp, err
}

func (c *client) ListSupportedModels(ctx context.Context) (*ProviderModels, error) {
	var models ProviderModels
	err := c.doRequest(ctx, "GET", "/models", nil, &models)
	return &models, err
}
