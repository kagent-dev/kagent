package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// shareClient holds the dependencies for share link tools, captured at construction time.
type shareClient struct {
	baseURL    string
	uiURL      string // KAGENT_UI_URL, used to build full share URLs
	appName    string
	httpClient *http.Client
}

// parseAppName converts a Python-identifier app_name back to (namespace, name).
// Format: "namespace__NS__agent_name" with hyphens encoded as underscores.
func parseAppName(appName string) (namespace, name string) {
	parts := strings.SplitN(appName, "__NS__", 2)
	if len(parts) != 2 {
		return "", strings.ReplaceAll(appName, "_", "-")
	}
	return strings.ReplaceAll(parts[0], "_", "-"), strings.ReplaceAll(parts[1], "_", "-")
}

// shareURL returns the share URL for a session token.
// With uiURL set it returns a full absolute URL; otherwise a relative path.
func (c *shareClient) shareURL(token, sessionID string) string {
	ns, name := parseAppName(c.appName)
	path := fmt.Sprintf("/agents/%s/%s/chat/%s?share=%s", ns, name, sessionID, token)
	if c.uiURL != "" {
		return c.uiURL + path
	}
	return path
}

func (c *shareClient) do(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request %s %s: %w", method, c.baseURL+path, err)
	}
	req.Header.Set("X-Agent-Name", c.appName)
	return c.httpClient.Do(req)
}

func (c *shareClient) doWithJSON(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("building request %s %s: %w", method, c.baseURL+path, err)
	}
	req.Header.Set("X-Agent-Name", c.appName)
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

func (c *shareClient) readBody(resp *http.Response) (map[string]any, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return out, nil
}

type createShareInput struct {
	// ReadOnly controls whether the shared link allows visitors to send messages.
	// When nil (not provided by the model), the server defaults to true (read-only).
	ReadOnly *bool `json:"read_only,omitempty"`
}

// NewCreateShareLinkTool creates a tool that generates a share token for the current session.
func NewCreateShareLinkTool(httpClient *http.Client, baseURL, appName string) (tool.Tool, error) {
	c := &shareClient{
		baseURL:    baseURL,
		uiURL:      strings.TrimRight(os.Getenv("KAGENT_UI_URL"), "/"),
		appName:    appName,
		httpClient: httpClient,
	}
	return functiontool.New(functiontool.Config{
		Name: "create_share_link",
		Description: "Creates a share link for the current chat session. " +
			"Returns a URL any authenticated user can open to view this session. " +
			"The link is read-only by default (visitors cannot send messages). " +
			"Set read_only=false to allow visitors to interact. " +
			"Each call creates a new token; existing tokens remain valid.",
	}, func(ctx agent.Context, in createShareInput) (map[string]any, error) {
		sessionID := ctx.SessionID()
		if sessionID == "" {
			return nil, fmt.Errorf("create_share_link: no session ID in context")
		}
		reqBody, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("create_share_link: encoding request: %w", err)
		}
		resp, err := c.doWithJSON(ctx, http.MethodPost, "/api/sessions/"+url.PathEscape(sessionID)+"/shares", strings.NewReader(string(reqBody)))
		if err != nil {
			return nil, fmt.Errorf("create_share_link: request failed: %w", err)
		}
		if resp.StatusCode != http.StatusCreated {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("create_share_link: unexpected status %d", resp.StatusCode)
		}
		body, err := c.readBody(resp)
		if err != nil {
			return nil, fmt.Errorf("create_share_link: %w", err)
		}
		data, _ := body["data"].(map[string]any)
		token, _ := data["token"].(string)
		readOnly, _ := data["read_only"].(bool)
		return map[string]any{
			"url":       c.shareURL(token, sessionID),
			"read_only": readOnly,
		}, nil
	})
}

// NewListShareLinksTool creates a tool that lists active share tokens for the current session.
func NewListShareLinksTool(httpClient *http.Client, baseURL, appName string) (tool.Tool, error) {
	c := &shareClient{
		baseURL:    baseURL,
		uiURL:      strings.TrimRight(os.Getenv("KAGENT_UI_URL"), "/"),
		appName:    appName,
		httpClient: httpClient,
	}
	return functiontool.New(functiontool.Config{
		Name: "list_share_links",
		Description: "Lists all active share links for the current session. " +
			"Returns each share token and creation time. " +
			"Use this to find a token before calling delete_share_link.",
	}, func(ctx agent.Context, _ struct{}) (map[string]any, error) {
		sessionID := ctx.SessionID()
		if sessionID == "" {
			return nil, fmt.Errorf("list_share_links: no session ID in context")
		}
		resp, err := c.do(ctx, http.MethodGet, "/api/sessions/"+url.PathEscape(sessionID)+"/shares")
		if err != nil {
			return nil, fmt.Errorf("list_share_links: request failed: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("list_share_links: unexpected status %d", resp.StatusCode)
		}
		body, err := c.readBody(resp)
		if err != nil {
			return nil, fmt.Errorf("list_share_links: %w", err)
		}
		shares := body["data"]
		if shares == nil {
			shares = []any{}
		}
		return map[string]any{"shares": shares}, nil
	})
}

type deleteShareInput struct {
	Token string `json:"token"`
}

// NewDeleteShareLinkTool creates a tool that revokes a specific share token for the current session.
func NewDeleteShareLinkTool(httpClient *http.Client, baseURL, appName string) (tool.Tool, error) {
	c := &shareClient{
		baseURL:    baseURL,
		uiURL:      strings.TrimRight(os.Getenv("KAGENT_UI_URL"), "/"),
		appName:    appName,
		httpClient: httpClient,
	}
	return functiontool.New(functiontool.Config{
		Name: "delete_share_link",
		Description: "Deletes a share link by token, immediately revoking access for anyone using it. " +
			"Use list_share_links first to find the token you want to revoke.",
	}, func(ctx agent.Context, in deleteShareInput) (map[string]any, error) {
		if in.Token == "" {
			return nil, fmt.Errorf("delete_share_link: token is required")
		}
		sessionID := ctx.SessionID()
		if sessionID == "" {
			return nil, fmt.Errorf("delete_share_link: no session ID in context")
		}
		path := "/api/sessions/" + url.PathEscape(sessionID) + "/shares/" + url.PathEscape(in.Token)
		resp, err := c.do(ctx, http.MethodDelete, path)
		if err != nil {
			return nil, fmt.Errorf("delete_share_link: request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("delete_share_link: unexpected status %d", resp.StatusCode)
		}
		return map[string]any{"status": "revoked", "token": in.Token}, nil
	})
}
