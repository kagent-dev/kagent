//go:build linux

package bridge

import (
	"bytes"
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

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/operations"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// Exercises real DSH and MCP with PostgreSQL surviving a reconstructed service
// and Runner. The A2A endpoint is a local fixture, not the kagent controller.
func TestRealDSHExternalApprovalAcrossRestart(t *testing.T) {
	executable, dsn := os.Getenv("DSH_TEST_BIN"), os.Getenv("DSH_APPROVAL_TEST_DATABASE")
	if executable == "" || dsn == "" {
		t.Skip("DSH_TEST_BIN and DSH_APPROVAL_TEST_DATABASE required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	const agentToken = "dsh-agent-token-012345678901234567890123"
	const approverToken = "human-approver-token-01234567890123456789"
	var effects atomic.Int32
	remote := mcp.NewServer(&mcp.Implementation{Name: "protected", Version: "1"}, nil)
	type deploymentInput struct {
		Version string `json:"version"`
	}
	mcp.AddTool(remote, &mcp.Tool{Name: "deploy"}, func(_ context.Context, _ *mcp.CallToolRequest, args deploymentInput) (*mcp.CallToolResult, map[string]any, error) {
		effects.Add(1)
		assert.Equal(t, "version-7", args.Version)
		return nil, map[string]any{"receipt": "deployment-receipt-7"}, nil
	})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer upstream.Close()
	var operationID atomic.Value
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid model request", 400)
			return
		}
		n := modelCalls.Add(1)
		if n == 1 || n == 2 || n == 4 {
			wanted := "request_operation"
			args := `{"request_key":"deploy-v7","tool":"deploy","arguments":{"version":"version-7"}}`
			if n == 1 {
				wanted = "list_operation_tools"
				args = `{}`
			}
			if n == 2 {
				assert.Contains(t, string(input.Messages), "version")
				assert.Contains(t, string(input.Messages), "inputSchema")
			}
			if n == 4 {
				wanted = "get_operation"
				args = fmt.Sprintf(`{"operation_id":%q}`, operationID.Load().(string))
			}
			name := ""
			for _, tool := range input.Tools {
				if strings.HasSuffix(tool.Function.Name, wanted) {
					name = tool.Function.Name
				}
			}
			if name == "" {
				http.Error(w, "missing operation tool", 500)
				return
			}
			writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("operation-call-%d", n), "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}, "tool_calls")
		} else {
			if n == 3 {
				assert.Contains(t, string(input.Messages), "awaiting_approval")
				assert.Zero(t, effects.Load())
			} else {
				assert.Contains(t, string(input.Messages), "deployment-receipt-7")
				assert.NotContains(t, string(input.Messages), "TOOL_OUTCOME_UNKNOWN")
			}
			writeCompletion(w, map[string]string{"role": "assistant", "content": "operation phase complete"}, "stop")
		}
	}))
	defer model.Close()
	var routed atomic.Pointer[http.Handler]
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := routed.Load()
		if h == nil {
			http.Error(w, "restarting", http.StatusServiceUnavailable)
			return
		}
		(*h).ServeHTTP(w, r)
	}))
	defer front.Close()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q,"headers":{"Authorization":%q}}}]}`, model.URL+"/v1", front.URL+"/mcp", "Bearer "+agentToken)))
	require.NoError(t, err)
	directory := t.TempDir()
	var nativeID string
	var callbackCount atomic.Int32
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage            `json:"id"`
			Params a2atype.SendMessageRequest `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid a2a request", 400)
			return
		}
		assert.Equal(t, "conversation-7", request.Params.Message.ContextID)
		assert.Empty(t, request.Params.Message.TaskID)
		callbackCount.Add(1)
		// New Runner and process; the same native session is loaded from DATA.
		runner, err := New(cfg, executable, directory, append(os.Environ(), "OPENAI_API_KEY=mock"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		sink := &recordingSink{}
		out, err := runner.Run(ctx, runtime.Turn{Prompt: "An approved external operation completed. Query its saved result.", ContinuationID: nativeID}, sink)
		if !assert.NoError(t, err) || !assert.Nil(t, out.Failure) {
			http.Error(w, "continuation failed", 500)
			return
		}
		assert.Equal(t, nativeID, sink.id)
		assert.Nil(t, out.Pending)
		assert.Len(t, sink.results, 1)
		result, err := json.Marshal(a2atype.StreamResponse{Event: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("continued"))})
		if err != nil {
			http.Error(w, "invalid result", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(request.ID) + `,"result":` + string(result) + `}`))
	}))
	defer callback.Close()
	serviceConfig := operations.Config{Scope: uuid.NewString(), UpstreamURL: upstream.URL, TargetRevision: "deploy-v1", Tools: []string{"deploy"}, Approver: "test-reviewer", ApprovalTTLSeconds: 3600, CallTimeoutSeconds: 5, Continuation: &operations.Continuation{URL: callback.URL, ContextID: "conversation-7"}}
	open := func() (*operations.Store, *operations.Service) {
		store, err := operations.OpenStore(ctx, dsn, serviceConfig.Scope)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		service, err := operations.New(store, serviceConfig, operations.NewMCPCaller(upstream.URL, "", 5*time.Second), operations.NewA2ANotifier(*serviceConfig.Continuation, ""))
		require.NoError(t, err)
		h, err := service.Handler(agentToken, approverToken)
		require.NoError(t, err)
		routed.Store(&h)
		return store, service
	}
	store, _ := open()
	runner, err := New(cfg, executable, directory, append(os.Environ(), "OPENAI_API_KEY=mock"))
	require.NoError(t, err)
	sink := &recordingSink{}
	out, err := runner.Run(ctx, runtime.Turn{Prompt: "Submit deployment version-7 for external approval, then stop."}, sink)
	require.NoError(t, err)
	require.Nil(t, out.Failure)
	require.Nil(t, out.Pending)
	require.Zero(t, effects.Load())
	require.Len(t, sink.results, 2)
	nativeID = sink.id
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, front.URL+"/operations", nil)
	require.NoError(t, err)
	get.Header.Set("Authorization", "Bearer "+approverToken)
	response, err := http.DefaultClient.Do(get)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	var saved []operations.Operation
	require.NoError(t, json.NewDecoder(response.Body).Decode(&saved))
	require.NoError(t, response.Body.Close())
	require.Len(t, saved, 1)
	operationID.Store(saved[0].ID)
	// The request survives both service reconstruction and the original DSH exit.
	require.NoError(t, store.Close())
	store, service := open()
	body := []byte(fmt.Sprintf(`{"request_digest":%q,"decision":"approved"}`, saved[0].RequestDigest))
	approve, err := http.NewRequestWithContext(ctx, http.MethodPost, front.URL+"/operations/"+saved[0].ID+"/decision", bytes.NewReader(body))
	require.NoError(t, err)
	approve.Header.Set("Authorization", "Bearer "+approverToken)
	response, err = http.DefaultClient.Do(approve)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.NoError(t, service.Tick(ctx))
	require.Equal(t, int32(1), effects.Load())
	require.Equal(t, int32(1), callbackCount.Load())
	require.Equal(t, int32(5), modelCalls.Load())
	result, err := store.Get(ctx, saved[0].ID)
	require.NoError(t, err)
	require.Equal(t, operations.Succeeded, result.State)
	require.Equal(t, "delivered", result.Notification)
	require.NoError(t, service.Tick(ctx))
	require.Equal(t, int32(1), effects.Load())
	require.Equal(t, int32(1), callbackCount.Load())
}
