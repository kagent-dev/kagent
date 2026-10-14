package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestRealDSHBuiltinToolsAndPermissions(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("DSH_TEST_BIN required")
	}
	for _, scenario := range []string{"bash", "approval-allow", "approval-reject", "approval-cancel", "malformed-arguments"} {
		t.Run(scenario, func(t *testing.T) {
			var requests atomic.Int32
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
					Messages json.RawMessage `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "bad request", 400)
					return
				}
				if requests.Add(1) == 1 {
					name := ""
					for _, tool := range body.Tools {
						if tool.Function.Name == "bash" {
							name = tool.Function.Name
						}
					}
					if name == "" {
						http.Error(w, "no bash tool", 500)
						return
					}
					args := `{"command":"printf audit-ok","description":"Print a harmless acceptance test marker"}`
					if strings.HasPrefix(scenario, "approval-") {
						args = `{"command":"printf audit-ok","description":"Print a harmless acceptance test marker","sandbox_permissions":"danger-full-access","justification":"Exercise approval with a harmless marker."}`
					}
					if scenario == "malformed-arguments" {
						args = `{"command":`
					}
					writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "audit-call", "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}, "tool_calls")
				} else {
					writeCompletion(w, map[string]string{"role": "assistant", "content": "audit complete"}, "stop")
				}
			}))
			defer model.Close()
			cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"}}`, model.URL+"/v1")))
			require.NoError(t, err)
			runner, err := New(cfg, executable, t.TempDir(), append(os.Environ(), "OPENAI_API_KEY=mock", "DSH_PERMISSION_MODE=workspace-write"))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
			defer cancel()
			segmentCtx, finishSegment := context.WithCancel(ctx)
			defer finishSegment()
			sink := &recordingSink{}
			out, err := runner.Run(segmentCtx, runtime.Turn{Prompt: "Run the acceptance check."}, sink)
			require.NoError(t, err)
			finishSegment()
			if strings.HasPrefix(scenario, "approval-") {
				require.NotNil(t, out.Pending, "real DSH must produce an approval request")
				if scenario == "approval-cancel" {
					require.NoError(t, out.Pending.Cancel(ctx, sink))
					resumed := &recordingSink{}
					out, err = runner.Run(ctx, runtime.Turn{Prompt: "Continue after permission cancellation.", ContinuationID: sink.id}, resumed)
					require.NoError(t, err)
					require.Nil(t, out.Failure)
					require.Nil(t, out.Pending)
					require.Equal(t, sink.id, resumed.id)
					require.Equal(t, []string{"audit complete"}, resumed.texts)
					require.Equal(t, int32(2), requests.Load())
					return
				}
				request := out.Pending.Request().(*runtime.ApprovalRequest)
				out, err = out.Pending.Resume(ctx, &runtime.ApprovalDecision{ID: request.ID, Approved: scenario == "approval-allow"}, sink)
				require.NoError(t, err)
			}
			require.Nil(t, out.Failure)
			require.Nil(t, out.Pending)
			require.Len(t, sink.results, 1)
			require.Equal(t, scenario == "approval-reject" || scenario == "malformed-arguments", sink.results[0].IsError)
			if scenario == "approval-reject" {
				require.Equal(t, runtime.ToolDeclined, sink.results[0].Status)
			}
			if scenario == "bash" || scenario == "approval-allow" {
				require.False(t, sink.results[0].IsError)
				require.Contains(t, fmt.Sprint(sink.results[0].Result), "audit-ok")
			}
			require.Equal(t, int32(2), requests.Load(), "tool failure should be returned to the model for recovery")
		})
	}
}

func TestRealDSHMCPErrorAndResumedToolCall(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("DSH_TEST_BIN required")
	}
	var calls atomic.Int32
	remote := mcp.NewServer(&mcp.Implementation{Name: "audit", Version: "1"}, nil)
	mcp.AddTool(remote, &mcp.Tool{Name: "audit_tool", Description: "Exercise tool errors and resumed calls."}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		n := calls.Add(1)
		if n == 1 {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "audit expected tool failure"}}}, nil, nil
		}
		return nil, map[string]any{"result": "resumed-tool-ok"}, nil
	})
	remoteHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer remoteHTTP.Close()
	var n atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		index := n.Add(1)
		if index == 1 || index == 3 {
			name := ""
			for _, tool := range body.Tools {
				if strings.HasSuffix(tool.Function.Name, "audit_tool") {
					name = tool.Function.Name
				}
			}
			writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call-%d", index), "type": "function", "function": map[string]string{"name": name, "arguments": "{}"}}}}, "tool_calls")
		} else {
			if index == 2 {
				assert.Contains(t, string(body.Messages), "audit expected tool failure")
			} else {
				assert.Contains(t, string(body.Messages), "resumed-tool-ok")
			}
			writeCompletion(w, map[string]string{"role": "assistant", "content": "settled"}, "stop")
		}
	}))
	defer model.Close()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q}}]}`, model.URL+"/v1", remoteHTTP.URL)))
	require.NoError(t, err)
	runner, err := New(cfg, executable, t.TempDir(), append(os.Environ(), "OPENAI_API_KEY=mock"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	id := ""
	for i := range 2 {
		sink := &recordingSink{}
		out, err := runner.Run(ctx, runtime.Turn{Prompt: "Call audit_tool.", ContinuationID: id}, sink)
		require.NoError(t, err)
		require.Nil(t, out.Failure)
		id = sink.id
		require.Len(t, sink.results, 1)
		require.Equal(t, i == 0, sink.results[0].IsError)
	}
	require.Equal(t, int32(2), calls.Load())
}
