package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestMCPAppModelResultReplacesRenderPayloadWithNotice(t *testing.T) {
	t.Parallel()

	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "jenkins_monitor_build",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": "Opened Jenkins Build Monitor for https://example.com/job/demo/1/ (current status: IN_PROGRESS).",
						}},
						"structuredContent": map[string]any{
							"build": map[string]any{
								"stages": []any{map[string]any{"name": "Deploy", "status": "IN_PROGRESS"}},
							},
							"polling_data": "large payload",
						},
						"_meta": map[string]any{
							"ui": map[string]any{
								"resourceUri": "ui://jenkins-mcp/build-monitor",
							},
						},
					},
				},
			}},
		}},
	}

	callback := makeMCPAppModelResultCallback(func(adkagent.Context) map[string]bool {
		return map[string]bool{"jenkins_monitor_build": true}
	})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response

	// Success render payload should be collapsed into the terminal notice so the
	// model stops re-invoking the rendering tool.
	content, ok := got["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content not replaced with notice: %#v", got["content"])
	}
	part, ok := content[0].(map[string]any)
	if !ok || part["text"] != mcpAppRenderedNotice {
		t.Fatalf("notice text missing: %#v", content[0])
	}

	// Should strip structuredContent (heavy render payload).
	if _, ok := got["structuredContent"]; ok {
		t.Fatalf("structuredContent should be stripped, got: %#v", got)
	}

	// Should preserve _meta
	meta, ok := got["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("_meta not preserved: %#v", got["_meta"])
	}
	if _, ok := meta["ui"]; !ok {
		t.Fatalf("_meta.ui not preserved: %#v", meta)
	}
}

func TestMCPAppModelResultPreservesIsError(t *testing.T) {
	t.Parallel()

	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name: "jenkins_monitor_build",
					Response: map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": "Tool execution failed.",
						}},
						"structuredContent": map[string]any{"error": "connection timeout"},
						"isError":           true,
					},
				},
			}},
		}},
	}

	callback := makeMCPAppModelResultCallback(func(adkagent.Context) map[string]bool {
		return map[string]bool{"jenkins_monitor_build": true}
	})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response

	// Should preserve isError
	isErr, ok := got["isError"].(bool)
	if !ok || !isErr {
		t.Fatalf("isError not preserved or false: %#v", got["isError"])
	}

	// Should still strip structuredContent
	if _, ok := got["structuredContent"]; ok {
		t.Fatalf("structuredContent should be stripped")
	}
}

func TestMCPAppModelResultKeepsToolErrorText(t *testing.T) {
	t.Parallel()

	// The ADK's response for a failed tool call is not an MCP result; its error
	// text must reach the model unchanged.
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name:     "jenkins_monitor_build",
					Response: map[string]any{"error": "connection timeout", "isError": true},
				},
			}},
		}},
	}

	callback := makeMCPAppModelResultCallback(func(adkagent.Context) map[string]bool {
		return map[string]bool{"jenkins_monitor_build": true}
	})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response
	want := map[string]any{"error": "connection timeout", "isError": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool error response = %#v, want %#v", got, want)
	}
}

func TestMCPAppModelResultLeavesNonAppToolsAlone(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"output": map[string]any{"answer": 42},
		"content": []map[string]any{{
			"type": "text",
			"text": "Answer is 42",
		}},
	}
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{{
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					Name:     "regular_tool",
					Response: original,
				},
			}},
		}},
	}

	callback := makeMCPAppModelResultCallback(func(adkagent.Context) map[string]bool {
		return map[string]bool{"some_app_tool": true}
	})
	if _, err := callback(nil, req); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	got := req.Contents[0].Parts[0].FunctionResponse.Response

	// Non-app tools should pass through unchanged
	if _, ok := got["output"]; !ok {
		t.Fatalf("non-app tool response modified: %#v", got)
	}
}

func TestMCPAppCompactionPreservesCacheApprovalAndHistoricalResults(t *testing.T) {
	// Given real MCP tools with approvals, when fresh and cached catalogs
	// prepare model results, assert that app compaction preserves rich results
	// and approval checks, including history after the app tool is removed.
	var lists, calls atomic.Int32
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "app-compaction", Version: "1"}, &mcpsdk.ServerOptions{
		SupportedProtocolVersions: []string{"2026-07-28"},
		SetCacheable:              func(_ context.Context, _ mcpsdk.Request, c *mcpsdk.Cacheable) { c.TTLMs = 60000 },
	})
	for _, descriptor := range []*mcpsdk.Tool{
		{Name: "ordinary"},
		{Name: "panel", Meta: mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://panel"}}},
		{Name: "app_only", Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}}},
	} {
		mcpsdk.AddTool(server, descriptor, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
			calls.Add(1)
			return nil, map[string]any{"payload": "rich result"}, nil
		})
	}
	server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/list" {
				lists.Add(1)
			}
			return next(ctx, method, req)
		}
	})
	srv := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	sets := mcp.CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: srv.URL}, RequireApproval: true}}, nil, nil, false, nil)
	require.Len(t, sets, 1)
	ctx := &appCompactionContext{StrictContextMock: adkagent.NewStrictContextMock(t.Context()), confirmed: true}
	callback := makeMCPAppModelResultCallback(func(ctx adkagent.Context) map[string]bool {
		return mcp.MCPAppToolNamesFromToolsets(ctx, sets)
	})
	var historical map[string]any
	for range 2 {
		listed, err := sets[0].Tools(ctx)
		require.NoError(t, err)
		require.Len(t, listed, 2)
		req := &adkmodel.LLMRequest{}
		original := make(map[string]map[string]any)
		for _, candidate := range listed {
			fn := candidate.(interface {
				ProcessRequest(adkagent.Context, *adkmodel.LLMRequest) error
				Run(adkagent.Context, any) (map[string]any, error)
			})
			require.NoError(t, fn.ProcessRequest(ctx, req))
			response, err := fn.Run(ctx, map[string]any{})
			require.NoError(t, err)
			original[candidate.Name()] = response
			req.Contents = append(req.Contents, &genai.Content{Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: candidate.Name(), Response: response}}}})
		}
		require.NotContains(t, req.Tools, "app_only")
		_, err = callback(ctx, req)
		require.NoError(t, err)
		for _, content := range req.Contents {
			response := content.Parts[0].FunctionResponse
			if response.Name == "panel" {
				require.Equal(t, mcpAppRenderedNotice, response.Response["content"].([]any)[0].(map[string]any)["text"])
			} else {
				require.Equal(t, original["ordinary"], response.Response)
			}
		}
		historical = original["panel"]
		require.Equal(t, map[string]any{"output": map[string]any{"payload": "rich result"}}, historical, "history retains the original rich result")
		ctx.confirmed = false
		_, err = req.Tools["panel"].(interface {
			Run(adkagent.Context, any) (map[string]any, error)
		}).Run(ctx, map[string]any{})
		require.ErrorIs(t, err, tool.ErrConfirmationRejected)
		ctx.confirmed = true
	}
	require.EqualValues(t, 1, lists.Load(), "cached model preparation makes no extra tools/list call")
	require.EqualValues(t, 4, calls.Load(), "rejected approvals must not execute MCP tools")
	server.RemoveTools("panel")
	require.Eventually(t, func() bool {
		listed, err := sets[0].Tools(ctx)
		require.NoError(t, err)
		return len(listed) == 1 && listed[0].Name() == "ordinary"
	}, 2*time.Second, 10*time.Millisecond)
	req := &adkmodel.LLMRequest{Contents: []*genai.Content{{Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "panel", Response: historical}}}}}}
	_, err := callback(ctx, req)
	require.NoError(t, err)
	require.Equal(t, mcpAppRenderedNotice, req.Contents[0].Parts[0].FunctionResponse.Response["content"].([]any)[0].(map[string]any)["text"])
	require.Equal(t, map[string]any{"output": map[string]any{"payload": "rich result"}}, historical)

	// Given the retained app name, when the tool returns as ordinary, assert
	// that its new output reaches the model without the rendered notice.
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "panel"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"payload": "ordinary result"}, nil
	})
	var ordinary tool.Tool
	require.Eventually(t, func() bool {
		listed, err := sets[0].Tools(ctx)
		require.NoError(t, err)
		for _, candidate := range listed {
			if candidate.Name() == "panel" {
				ordinary = candidate
				return !mcp.MCPAppToolNamesFromToolsets(ctx, sets)["panel"]
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
	response, err := ordinary.(interface {
		Run(adkagent.Context, any) (map[string]any, error)
	}).Run(ctx, map[string]any{})
	require.NoError(t, err)
	req = &adkmodel.LLMRequest{Contents: []*genai.Content{{Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "panel", Response: response}}}}}}
	_, err = callback(ctx, req)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"output": map[string]any{"payload": "ordinary result"}}, req.Contents[0].Parts[0].FunctionResponse.Response)
}

type appCompactionContext struct {
	adkagent.StrictContextMock
	confirmed bool
}

func (c *appCompactionContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return &toolconfirmation.ToolConfirmation{Confirmed: c.confirmed}
}

func (*appCompactionContext) FunctionCallID() string { return "app-compaction-call" }
