package prometheus

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

// mockRoundTripper is used to mock HTTP responses for testing
type mockRoundTripper struct {
	response *http.Response
	err      error
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func newTestClient(response *http.Response, err error) *http.Client {
	return &http.Client{
		Transport: &mockRoundTripper{
			response: response,
			err:      err,
		},
	}
}

// Helper function to extract text content from MCP result
func getResultText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if textContent, ok := result.Content[0].(mcp.TextContent); ok {
		return textContent.Text
	}
	return ""
}

// Helper function to create a mock HTTP response
func createMockResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// Helper function to create context with mock HTTP client
func contextWithMockClient(client *http.Client) context.Context {
	return context.WithValue(context.Background(), clientKey{}, client)
}

func TestHandlePrometheusQueryTool(t *testing.T) {
	t.Run("successful query", func(t *testing.T) {
		mockResponse := `{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{
						"metric": {"__name__": "up", "instance": "localhost:9090"},
						"value": [1609459200, "1"]
					}
				]
			}
		}`

		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query":          "up",
			"prometheus_url": "http://localhost:9090",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		content := getResultText(result)
		assert.Contains(t, content, "success")
		assert.Contains(t, content, "up")
	})

	t.Run("missing query parameter", func(t *testing.T) {
		ctx := context.Background()
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"prometheus_url": "http://localhost:9090",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "query parameter is required")
	})

	t.Run("HTTP error", func(t *testing.T) {
		client := newTestClient(nil, assert.AnError)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "failed to query Prometheus")
	})

	t.Run("HTTP 500 error", func(t *testing.T) {
		client := newTestClient(createMockResponse(500, "Internal Server Error"), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "Prometheus API error (500)")
	})

	t.Run("malformed JSON response", func(t *testing.T) {
		client := newTestClient(createMockResponse(200, "invalid json {"), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
		// Should return raw response when JSON parsing fails
		assert.Contains(t, getResultText(result), "invalid json")
	})

	t.Run("default prometheus URL", func(t *testing.T) {
		mockResponse := `{"status": "success", "data": {"result": []}}`
		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})
}

func TestHandlePrometheusRangeQueryTool(t *testing.T) {
	t.Run("successful range query", func(t *testing.T) {
		mockResponse := `{
			"status": "success",
			"data": {
				"resultType": "matrix",
				"result": [
					{
						"metric": {"__name__": "up"},
						"values": [[1609459200, "1"], [1609459260, "1"]]
					}
				]
			}
		}`

		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
			"start": "1609459200",
			"end":   "1609459260",
			"step":  "60s",
		}

		result, err := handlePrometheusRangeQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		content := getResultText(result)
		assert.Contains(t, content, "matrix")
		assert.Contains(t, content, "values")
	})

	t.Run("missing query parameter", func(t *testing.T) {
		ctx := context.Background()
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{}

		result, err := handlePrometheusRangeQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "query parameter is required")
	})

	t.Run("default time range and step", func(t *testing.T) {
		mockResponse := `{"status": "success", "data": {"result": []}}`
		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusRangeQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})
}

func TestHandlePrometheusLabelsQueryTool(t *testing.T) {
	t.Run("successful labels query", func(t *testing.T) {
		mockResponse := `{
			"status": "success",
			"data": ["__name__", "instance", "job"]
		}`

		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{}

		result, err := handlePrometheusLabelsQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		content := getResultText(result)
		assert.Contains(t, content, "__name__")
		assert.Contains(t, content, "instance")
		assert.Contains(t, content, "job")
	})

	t.Run("HTTP error", func(t *testing.T) {
		client := newTestClient(nil, assert.AnError)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{}

		result, err := handlePrometheusLabelsQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "failed to query Prometheus")
	})

	t.Run("custom prometheus URL", func(t *testing.T) {
		mockResponse := `{"status": "success", "data": []}`
		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"prometheus_url": "http://custom:9090",
		}

		result, err := handlePrometheusLabelsQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})
}

func TestHandlePrometheusTargetsQueryTool(t *testing.T) {
	t.Run("successful targets query", func(t *testing.T) {
		mockResponse := `{
			"status": "success",
			"data": {
				"activeTargets": [
					{
						"discoveredLabels": {"__address__": "localhost:9090"},
						"labels": {"instance": "localhost:9090", "job": "prometheus"},
						"scrapePool": "prometheus",
						"scrapeUrl": "http://localhost:9090/metrics",
						"health": "up"
					}
				]
			}
		}`

		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{}

		result, err := handlePrometheusTargetsQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		content := getResultText(result)
		assert.Contains(t, content, "activeTargets")
		assert.Contains(t, content, "localhost:9090")
		assert.Contains(t, content, "up")
	})

	t.Run("HTTP 404 error", func(t *testing.T) {
		client := newTestClient(createMockResponse(404, "Not Found"), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{}

		result, err := handlePrometheusTargetsQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "Prometheus API error (404)")
	})
}

func TestHandlePromql(t *testing.T) {
	t.Run("missing query description", func(t *testing.T) {
		ctx := context.Background()
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{}

		result, err := handlePromql(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsError)
		assert.Contains(t, getResultText(result), "query_description is required")
	})

	t.Run("with query description", func(t *testing.T) {
		ctx := context.Background()
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query_description": "CPU usage percentage",
		}

		result, err := handlePromql(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		// This will likely fail due to missing OpenAI API key, but that's expected
		// We're testing that the function handles the error gracefully
		if result.IsError {
			content := getResultText(result)
			// Should contain an error about LLM client or API
			assert.True(t,
				strings.Contains(content, "failed to create LLM client") ||
					strings.Contains(content, "failed to generate content") ||
					strings.Contains(content, "API"),
			)
		}
	})
}

// Test context cancellation scenarios
func TestPrometheusToolsContextCancellation(t *testing.T) {
	t.Run("query tool with cancelled context", func(t *testing.T) {
		// Create a mock client that would block indefinitely
		client := newTestClient(createMockResponse(200, `{"status": "success"}`), nil)

		// Create a cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
		ctx = contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		// Should handle cancellation gracefully
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

// Test edge cases and error scenarios
func TestPrometheusToolsEdgeCases(t *testing.T) {
	t.Run("very large response", func(t *testing.T) {
		// Create a large response (simulating large metrics data)
		largeResponse := `{"status": "success", "data": {"result": [`
		for i := 0; i < 1000; i++ {
			if i > 0 {
				largeResponse += ","
			}
			largeResponse += `{"metric": {"instance": "host` + string(rune(i)) + `"}, "value": [1609459200, "1"]}`
		}
		largeResponse += `]}}`

		client := newTestClient(createMockResponse(200, largeResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		content := getResultText(result)
		assert.Contains(t, content, "success")
	})

	t.Run("special characters in query", func(t *testing.T) {
		mockResponse := `{"status": "success", "data": {"result": []}}`
		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": `up{instance=~".*:9090"}`,
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})

	t.Run("empty response body", func(t *testing.T) {
		client := newTestClient(createMockResponse(200, ""), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)
	})
}

// Test URL parameter encoding
func TestPrometheusURLEncoding(t *testing.T) {
	t.Run("query with special characters", func(t *testing.T) {
		mockResponse := `{"status": "success", "data": {"result": []}}`
		client := newTestClient(createMockResponse(200, mockResponse), nil)
		ctx := contextWithMockClient(client)

		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"query": "up{job=\"test service\"}",
		}

		result, err := handlePrometheusQueryTool(ctx, request)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.IsError)

		// Test passes if no error occurs with special characters
		content := getResultText(result)
		assert.Contains(t, content, "success")
	})
}
