package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Grafana API client

type GrafanaConfig struct {
	BaseURL  string
	Username string
	Password string
	APIKey   string
}

func newGrafanaClient(baseURL, username, password, apiKey string) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

func makeGrafanaRequest(method, baseURL, endpoint, username, password, apiKey string, body interface{}) ([]byte, error) {
	client := newGrafanaClient(baseURL, username, password, apiKey)

	var reqBody []byte
	var err error
	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	fullURL := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(endpoint, "/")
	req, err := http.NewRequest(method, fullURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func handleGrafanaOrgManagement(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	baseURL := mcp.ParseString(request, "base_url", "http://localhost:3000/api")
	username := mcp.ParseString(request, "username", "")
	password := mcp.ParseString(request, "password", "")
	apiKey := mcp.ParseString(request, "api_key", "")
	action := mcp.ParseString(request, "action", "")

	if action == "" {
		return mcp.NewToolResultError("action parameter is required"), nil
	}

	var endpoint string
	var method string
	var requestBody interface{}

	switch action {
	case "list":
		endpoint = "orgs"
		method = "GET"
	case "get_current":
		endpoint = "org"
		method = "GET"
	case "create":
		orgName := mcp.ParseString(request, "org_name", "")
		if orgName == "" {
			return mcp.NewToolResultError("org_name is required for create action"), nil
		}
		endpoint = "orgs"
		method = "POST"
		requestBody = map[string]string{"name": orgName}
	case "update":
		orgID := mcp.ParseString(request, "org_id", "")
		orgName := mcp.ParseString(request, "org_name", "")
		if orgID == "" || orgName == "" {
			return mcp.NewToolResultError("org_id and org_name are required for update action"), nil
		}
		endpoint = "orgs/" + orgID
		method = "PUT"
		requestBody = map[string]string{"name": orgName}
	case "delete":
		orgID := mcp.ParseString(request, "org_id", "")
		if orgID == "" {
			return mcp.NewToolResultError("org_id is required for delete action"), nil
		}
		endpoint = "orgs/" + orgID
		method = "DELETE"
	default:
		return mcp.NewToolResultError("unsupported action: " + action), nil
	}

	respBody, err := makeGrafanaRequest(method, baseURL, endpoint, username, password, apiKey, requestBody)
	if err != nil {
		return mcp.NewToolResultError("Grafana API request failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(string(respBody)), nil
}

func handleGrafanaDashboardManagement(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	baseURL := mcp.ParseString(request, "base_url", "http://localhost:3000/api")
	username := mcp.ParseString(request, "username", "")
	password := mcp.ParseString(request, "password", "")
	apiKey := mcp.ParseString(request, "api_key", "")
	action := mcp.ParseString(request, "action", "")

	if action == "" {
		return mcp.NewToolResultError("action parameter is required"), nil
	}

	var endpoint string
	var method string
	var requestBody interface{}

	switch action {
	case "search":
		query := mcp.ParseString(request, "query", "")
		endpoint = "search"
		method = "GET"
		if query != "" {
			endpoint += "?query=" + url.QueryEscape(query)
		}
	case "get":
		uid := mcp.ParseString(request, "uid", "")
		if uid == "" {
			return mcp.NewToolResultError("uid is required for get action"), nil
		}
		endpoint = "dashboards/uid/" + uid
		method = "GET"
	case "create":
		dashboardJSON := mcp.ParseString(request, "dashboard_json", "")
		if dashboardJSON == "" {
			return mcp.NewToolResultError("dashboard_json is required for create action"), nil
		}
		endpoint = "dashboards/db"
		method = "POST"

		var dashboard map[string]interface{}
		if err := json.Unmarshal([]byte(dashboardJSON), &dashboard); err != nil {
			return mcp.NewToolResultError("invalid dashboard JSON: " + err.Error()), nil
		}
		requestBody = map[string]interface{}{
			"dashboard": dashboard,
			"overwrite": true,
		}
	case "delete":
		uid := mcp.ParseString(request, "uid", "")
		if uid == "" {
			return mcp.NewToolResultError("uid is required for delete action"), nil
		}
		endpoint = "dashboards/uid/" + uid
		method = "DELETE"
	default:
		return mcp.NewToolResultError("unsupported action: " + action), nil
	}

	respBody, err := makeGrafanaRequest(method, baseURL, endpoint, username, password, apiKey, requestBody)
	if err != nil {
		return mcp.NewToolResultError("Grafana API request failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(string(respBody)), nil
}

func handleGrafanaAlertManagement(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	baseURL := mcp.ParseString(request, "base_url", "http://localhost:3000/api")
	username := mcp.ParseString(request, "username", "")
	password := mcp.ParseString(request, "password", "")
	apiKey := mcp.ParseString(request, "api_key", "")
	action := mcp.ParseString(request, "action", "")

	if action == "" {
		return mcp.NewToolResultError("action parameter is required"), nil
	}

	var endpoint string
	var method string

	switch action {
	case "list":
		endpoint = "alerts"
		method = "GET"
	case "list_rules":
		endpoint = "ruler/grafana/api/v1/rules"
		method = "GET"
	case "get_rule":
		ruleUID := mcp.ParseString(request, "rule_uid", "")
		if ruleUID == "" {
			return mcp.NewToolResultError("rule_uid is required for get_rule action"), nil
		}
		endpoint = "ruler/grafana/api/v1/rules/" + ruleUID
		method = "GET"
	case "pause":
		ruleUID := mcp.ParseString(request, "rule_uid", "")
		if ruleUID == "" {
			return mcp.NewToolResultError("rule_uid is required for pause action"), nil
		}
		endpoint = "ruler/grafana/api/v1/rules/" + ruleUID + "/pause"
		method = "POST"
	case "unpause":
		ruleUID := mcp.ParseString(request, "rule_uid", "")
		if ruleUID == "" {
			return mcp.NewToolResultError("rule_uid is required for unpause action"), nil
		}
		endpoint = "ruler/grafana/api/v1/rules/" + ruleUID + "/unpause"
		method = "POST"
	default:
		return mcp.NewToolResultError("unsupported action: " + action), nil
	}

	respBody, err := makeGrafanaRequest(method, baseURL, endpoint, username, password, apiKey, nil)
	if err != nil {
		return mcp.NewToolResultError("Grafana API request failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(string(respBody)), nil
}

func handleGrafanaDataSourceManagement(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	baseURL := mcp.ParseString(request, "base_url", "http://localhost:3000/api")
	username := mcp.ParseString(request, "username", "")
	password := mcp.ParseString(request, "password", "")
	apiKey := mcp.ParseString(request, "api_key", "")
	action := mcp.ParseString(request, "action", "")

	if action == "" {
		return mcp.NewToolResultError("action parameter is required"), nil
	}

	var endpoint string
	var method string
	var requestBody interface{}

	switch action {
	case "list":
		endpoint = "datasources"
		method = "GET"
	case "get":
		datasourceID := mcp.ParseString(request, "datasource_id", "")
		if datasourceID == "" {
			return mcp.NewToolResultError("datasource_id is required for get action"), nil
		}
		endpoint = "datasources/" + datasourceID
		method = "GET"
	case "create":
		name := mcp.ParseString(request, "name", "")
		dsType := mcp.ParseString(request, "type", "")
		dsURL := mcp.ParseString(request, "url", "")
		if name == "" || dsType == "" || dsURL == "" {
			return mcp.NewToolResultError("name, type, and url are required for create action"), nil
		}
		endpoint = "datasources"
		method = "POST"
		requestBody = map[string]interface{}{
			"name": name,
			"type": dsType,
			"url":  dsURL,
		}
	case "delete":
		datasourceID := mcp.ParseString(request, "datasource_id", "")
		if datasourceID == "" {
			return mcp.NewToolResultError("datasource_id is required for delete action"), nil
		}
		endpoint = "datasources/" + datasourceID
		method = "DELETE"
	default:
		return mcp.NewToolResultError("unsupported action: " + action), nil
	}

	respBody, err := makeGrafanaRequest(method, baseURL, endpoint, username, password, apiKey, requestBody)
	if err != nil {
		return mcp.NewToolResultError("Grafana API request failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(string(respBody)), nil
}

func RegisterGrafanaTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("grafana_org_management",
		mcp.WithDescription("Manage Grafana organizations"),
		mcp.WithString("base_url", mcp.Description("The base URL of the Grafana API")),
		mcp.WithString("username", mcp.Description("Username for basic auth")),
		mcp.WithString("password", mcp.Description("Password for basic auth")),
		mcp.WithString("api_key", mcp.Description("API key for token auth")),
		mcp.WithString("action", mcp.Description("Action to perform: list, get_current, create, update, delete"), mcp.Required()),
		mcp.WithString("org_id", mcp.Description("Organization ID (required for update/delete)")),
		mcp.WithString("org_name", mcp.Description("Organization name (required for create/update)")),
	), handleGrafanaOrgManagement)

	s.AddTool(mcp.NewTool("grafana_dashboard_management",
		mcp.WithDescription("Manage Grafana dashboards"),
		mcp.WithString("base_url", mcp.Description("The base URL of the Grafana API")),
		mcp.WithString("username", mcp.Description("Username for basic auth")),
		mcp.WithString("password", mcp.Description("Password for basic auth")),
		mcp.WithString("api_key", mcp.Description("API key for token auth")),
		mcp.WithString("action", mcp.Description("Action to perform: search, get, create, delete"), mcp.Required()),
		mcp.WithString("query", mcp.Description("Search query (for search action)")),
		mcp.WithString("uid", mcp.Description("Dashboard UID (required for get/delete)")),
		mcp.WithString("dashboard_json", mcp.Description("Dashboard JSON (required for create)")),
	), handleGrafanaDashboardManagement)

	s.AddTool(mcp.NewTool("grafana_alert_management",
		mcp.WithDescription("Manage Grafana alerts and alert rules"),
		mcp.WithString("base_url", mcp.Description("The base URL of the Grafana API")),
		mcp.WithString("username", mcp.Description("Username for basic auth")),
		mcp.WithString("password", mcp.Description("Password for basic auth")),
		mcp.WithString("api_key", mcp.Description("API key for token auth")),
		mcp.WithString("action", mcp.Description("Action to perform: list, list_rules, get_rule, pause, unpause"), mcp.Required()),
		mcp.WithString("rule_uid", mcp.Description("Alert rule UID (required for get_rule/pause/unpause)")),
	), handleGrafanaAlertManagement)

	s.AddTool(mcp.NewTool("grafana_datasource_management",
		mcp.WithDescription("Manage Grafana data sources"),
		mcp.WithString("base_url", mcp.Description("The base URL of the Grafana API")),
		mcp.WithString("username", mcp.Description("Username for basic auth")),
		mcp.WithString("password", mcp.Description("Password for basic auth")),
		mcp.WithString("api_key", mcp.Description("API key for token auth")),
		mcp.WithString("action", mcp.Description("Action to perform: list, get, create, delete"), mcp.Required()),
		mcp.WithString("datasource_id", mcp.Description("Data source ID (required for get/delete)")),
		mcp.WithString("name", mcp.Description("Data source name (required for create)")),
		mcp.WithString("type", mcp.Description("Data source type (required for create)")),
		mcp.WithString("url", mcp.Description("Data source URL (required for create)")),
	), handleGrafanaDataSourceManagement)
}
