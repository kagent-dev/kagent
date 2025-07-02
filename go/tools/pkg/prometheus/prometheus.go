package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// clientKey is the context key for the http client.
type clientKey struct{}

func getHTTPClient(ctx context.Context) *http.Client {
	if client, ok := ctx.Value(clientKey{}).(*http.Client); ok && client != nil {
		return client
	}
	return http.DefaultClient
}

// Prometheus tools using direct HTTP API calls

func handlePrometheusQueryTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	prometheusURL := mcp.ParseString(request, "prometheus_url", "http://localhost:9090")
	query := mcp.ParseString(request, "query", "")

	if query == "" {
		return mcp.NewToolResultError("query parameter is required"), nil
	}

	// Make request to Prometheus API
	apiURL := fmt.Sprintf("%s/api/v1/query", prometheusURL)
	params := url.Values{}
	params.Add("query", query)
	params.Add("time", fmt.Sprintf("%d", time.Now().Unix()))

	fullURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	client := getHTTPClient(ctx)
	resp, err := client.Get(fullURL)
	if err != nil {
		return mcp.NewToolResultError("failed to query Prometheus: " + err.Error()), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError("failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("Prometheus API error (%d): %s", resp.StatusCode, string(body))), nil
	}

	// Parse the JSON response to pretty-print it
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	return mcp.NewToolResultText(string(prettyJSON)), nil
}

func handlePrometheusRangeQueryTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	prometheusURL := mcp.ParseString(request, "prometheus_url", "http://localhost:9090")
	query := mcp.ParseString(request, "query", "")
	start := mcp.ParseString(request, "start", "")
	end := mcp.ParseString(request, "end", "")
	step := mcp.ParseString(request, "step", "15s")

	if query == "" {
		return mcp.NewToolResultError("query parameter is required"), nil
	}

	// Use default time range if not specified
	if start == "" {
		start = fmt.Sprintf("%d", time.Now().Add(-1*time.Hour).Unix())
	}
	if end == "" {
		end = fmt.Sprintf("%d", time.Now().Unix())
	}

	// Make request to Prometheus API
	apiURL := fmt.Sprintf("%s/api/v1/query_range", prometheusURL)
	params := url.Values{}
	params.Add("query", query)
	params.Add("start", start)
	params.Add("end", end)
	params.Add("step", step)

	fullURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	client := getHTTPClient(ctx)
	resp, err := client.Get(fullURL)
	if err != nil {
		return mcp.NewToolResultError("failed to query Prometheus: " + err.Error()), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError("failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("Prometheus API error (%d): %s", resp.StatusCode, string(body))), nil
	}

	// Parse the JSON response to pretty-print it
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	return mcp.NewToolResultText(string(prettyJSON)), nil
}

func handlePrometheusLabelsQueryTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	prometheusURL := mcp.ParseString(request, "prometheus_url", "http://localhost:9090")

	// Make request to Prometheus API for labels
	apiURL := fmt.Sprintf("%s/api/v1/labels", prometheusURL)

	client := getHTTPClient(ctx)
	resp, err := client.Get(apiURL)
	if err != nil {
		return mcp.NewToolResultError("failed to query Prometheus: " + err.Error()), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError("failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("Prometheus API error (%d): %s", resp.StatusCode, string(body))), nil
	}

	// Parse the JSON response to pretty-print it
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	return mcp.NewToolResultText(string(prettyJSON)), nil
}

func handlePrometheusTargetsQueryTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	prometheusURL := mcp.ParseString(request, "prometheus_url", "http://localhost:9090")

	// Make request to Prometheus API for targets
	apiURL := fmt.Sprintf("%s/api/v1/targets", prometheusURL)

	client := getHTTPClient(ctx)
	resp, err := client.Get(apiURL)
	if err != nil {
		return mcp.NewToolResultError("failed to query Prometheus: " + err.Error()), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError("failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("Prometheus API error (%d): %s", resp.StatusCode, string(body))), nil
	}

	// Parse the JSON response to pretty-print it
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	prettyJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(string(body)), nil
	}

	return mcp.NewToolResultText(string(prettyJSON)), nil
}

func RegisterPrometheusTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("prometheus_query_tool",
		mcp.WithDescription("Execute a PromQL query against Prometheus"),
		mcp.WithString("query", mcp.Description("PromQL query to execute"), mcp.Required()),
		mcp.WithString("prometheus_url", mcp.Description("Prometheus server URL (default: http://localhost:9090)")),
	), handlePrometheusQueryTool)

	s.AddTool(mcp.NewTool("prometheus_query_range_tool",
		mcp.WithDescription("Execute a PromQL range query against Prometheus"),
		mcp.WithString("query", mcp.Description("PromQL query to execute"), mcp.Required()),
		mcp.WithString("start", mcp.Description("Start time (Unix timestamp or relative time)")),
		mcp.WithString("end", mcp.Description("End time (Unix timestamp or relative time)")),
		mcp.WithString("step", mcp.Description("Query resolution step (default: 15s)")),
		mcp.WithString("prometheus_url", mcp.Description("Prometheus server URL (default: http://localhost:9090)")),
	), handlePrometheusRangeQueryTool)

	s.AddTool(mcp.NewTool("prometheus_label_names_tool",
		mcp.WithDescription("Get all available labels from Prometheus"),
		mcp.WithString("prometheus_url", mcp.Description("Prometheus server URL (default: http://localhost:9090)")),
	), handlePrometheusLabelsQueryTool)

	s.AddTool(mcp.NewTool("prometheus_targets_tool",
		mcp.WithDescription("Get all Prometheus targets and their status"),
		mcp.WithString("prometheus_url", mcp.Description("Prometheus server URL (default: http://localhost:9090)")),
	), handlePrometheusTargetsQueryTool)

	s.AddTool(mcp.NewTool("prometheus_promql_tool",
		mcp.WithDescription("Generate a PromQL query"),
		mcp.WithString("query_description", mcp.Description("A string describing the query to generate"), mcp.Required()),
	), handlePromql)
}
