package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	kagentmcp "github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

type metadataIntegrationToolContext struct {
	adkagent.Context
	requestContext context.Context
}

func (c metadataIntegrationToolContext) Deadline() (time.Time, bool) {
	return c.requestContext.Deadline()
}
func (c metadataIntegrationToolContext) Done() <-chan struct{}  { return c.requestContext.Done() }
func (c metadataIntegrationToolContext) Err() error             { return c.requestContext.Err() }
func (c metadataIntegrationToolContext) Value(key any) any      { return c.requestContext.Value(key) }
func (c metadataIntegrationToolContext) SessionID() string      { return "metadata-conversation" }
func (c metadataIntegrationToolContext) InvocationID() string   { return "metadata-invocation" }
func (c metadataIntegrationToolContext) FunctionCallID() string { return "metadata-call" }
func (c metadataIntegrationToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return nil
}

type metadataIntegrationExecutor struct {
	toolset tool.Toolset
	hitl    chan bool
	pause   atomic.Bool
}

func (e *metadataIntegrationExecutor) Execute(ctx context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if request.StoredTask == nil && !yield(a2atype.NewSubmittedTask(request, request.Message), nil) {
			return
		}
		e.hitl <- HitlActivated(ctx)
		if e.pause.Swap(false) {
			message := a2atype.NewMessage(a2atype.MessageRoleAgent, confirmationPart("metadata-confirmation", "echo", "metadata-call", map[string]any{}, nil))
			yield(a2atype.NewStatusUpdateEvent(request, a2atype.TaskStateInputRequired, message), nil)
			return
		}
		toolContext := metadataIntegrationToolContext{requestContext: ctx}
		tools, err := e.toolset.Tools(toolContext)
		if err != nil {
			yield(nil, err)
			return
		}
		if len(tools) != 1 {
			yield(nil, fmt.Errorf("got %d tools, want one", len(tools)))
			return
		}
		runnable, ok := tools[0].(interface {
			Run(adkagent.Context, any) (map[string]any, error)
		})
		if !ok {
			yield(nil, fmt.Errorf("tool %T cannot run", tools[0]))
			return
		}
		if _, err := runnable.Run(toolContext, map[string]any{}); err != nil {
			yield(nil, err)
			return
		}
		yield(a2atype.NewStatusUpdateEvent(request, a2atype.TaskStateCompleted, nil), nil)
	}
}

func (*metadataIntegrationExecutor) Cancel(_ context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		yield(a2atype.NewStatusUpdateEvent(request, a2atype.TaskStateCanceled, nil), nil)
	}
}

func TestA2AToolCallMetadataReachesMCPPerRequest(t *testing.T) {
	capabilities := &mcpsdk.ServerCapabilities{}
	capabilities.AddExtension(apia2a.MCPToolCallMetadataExtensionID, nil)
	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "metadata", Version: "1"}, &mcpsdk.ServerOptions{Capabilities: capabilities})
	observedMetadata := make(chan map[string]any, 1)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "echo"}, func(_ context.Context, request *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		observedMetadata <- request.Params.GetMeta()
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}, nil, nil
	})
	mcpHTTP := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return mcpServer }, &mcpsdk.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(func() {
		mcpHTTP.CloseClientConnections()
		mcpHTTP.Close()
	})
	toolsets := kagentmcp.CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{
		Params: &configv1.ADKHTTPConnectionParams{Url: mcpHTTP.URL},
	}}, nil, nil, false, nil)
	require.Len(t, toolsets, 1)
	builtin := &metadataIntegrationExecutor{toolset: toolsets[0], hitl: make(chan bool, 1)}
	executor := &KAgentExecutor{builtin: builtin, logger: slog.New(slog.DiscardHandler)}
	handler := a2asrv.NewHandler(executor, a2asrv.WithCallInterceptors(HITLActivationInterceptor(), MCPToolCallMetadataActivationInterceptor()))
	a2aHTTP := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	a2aHTTP.Client().Timeout = 10 * time.Second
	t.Cleanup(a2aHTTP.Close)

	for _, test := range []struct {
		name      string
		activated bool
		metadata  any
		want      map[string]any
	}{
		{name: "first turn", activated: true, metadata: map[string]any{toolCallMetadataSessionKey: "first", "format": "csv"}, want: map[string]any{toolCallMetadataSessionKey: "first", "format": "csv"}},
		{name: "no activation", metadata: map[string]any{toolCallMetadataSessionKey: "ignored"}},
		{name: "empty turn", activated: true},
		{name: "next turn", activated: true, metadata: map[string]any{toolCallMetadataSessionKey: "next"}, want: map[string]any{toolCallMetadataSessionKey: "next"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := sendMetadataIntegrationRequest(t, a2aHTTP, test.activated, test.metadata, nil)
			require.Nil(t, response.Error, "A2A response: %s", response.Result)
			require.Len(t, builtin.hitl, 1)
			require.True(t, <-builtin.hitl)
			require.Len(t, observedMetadata, 1, "A2A response: %s", response.Result)
			metadata := <-observedMetadata
			if test.want == nil {
				require.NotContains(t, metadata, apia2a.MCPToolCallMetadataExtensionID)
			} else {
				require.Equal(t, test.want, metadata[apia2a.MCPToolCallMetadataExtensionID])
			}
		})
	}
	response := sendMetadataIntegrationRequest(t, a2aHTTP, true, "not an object", nil)
	require.NotNil(t, response.Error)
	require.Equal(t, -32602, response.Error.Code)
	require.Empty(t, builtin.hitl)
	require.Empty(t, observedMetadata)

	builtin.pause.Store(true)
	response = sendMetadataIntegrationRequest(t, a2aHTTP, true, map[string]any{toolCallMetadataSessionKey: "before-approval"}, nil)
	require.Nil(t, response.Error)
	require.Len(t, builtin.hitl, 1)
	require.True(t, <-builtin.hitl)
	require.Empty(t, observedMetadata)
	var pause a2atype.StreamResponse
	require.NoError(t, json.Unmarshal(response.Result, &pause))
	task, ok := pause.Event.(*a2atype.Task)
	require.True(t, ok, "A2A response: %s", response.Result)
	require.Equal(t, a2atype.TaskStateInputRequired, task.Status.State)
	decision := hitlDecisionMessage(&apia2a.ToolApprovalResponse{
		Type: HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: "metadata-confirmation", Approved: true}},
	})
	decision.TaskID, decision.ContextID = task.ID, task.ContextID
	response = sendMetadataIntegrationRequest(t, a2aHTTP, true, map[string]any{toolCallMetadataSessionKey: "after-approval"}, decision)
	require.Nil(t, response.Error)
	require.Len(t, builtin.hitl, 1)
	require.True(t, <-builtin.hitl)
	require.Len(t, observedMetadata, 1, "A2A response: %s", response.Result)
	require.Equal(t, map[string]any{toolCallMetadataSessionKey: "after-approval"}, (<-observedMetadata)[apia2a.MCPToolCallMetadataExtensionID])
}

type metadataIntegrationResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func sendMetadataIntegrationRequest(t *testing.T, server *httptest.Server, activated bool, metadata any, message *a2atype.Message) metadataIntegrationResponse {
	t.Helper()
	if message == nil {
		message = a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("call echo"))
	}
	message.ContextID = "metadata-conversation"
	request := &a2atype.SendMessageRequest{Message: message}
	if metadata != nil {
		request.Metadata = map[string]any{apia2a.MCPToolCallMetadataExtensionURI: metadata}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": message.ID, "method": "SendMessage", "params": request})
	require.NoError(t, err)
	httpRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))
	extensions := HITLExtensionURI
	if activated {
		extensions += ", " + apia2a.MCPToolCallMetadataExtensionURI
	}
	httpRequest.Header.Set(a2atype.SvcParamExtensions, extensions)
	response, err := server.Client().Do(httpRequest)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var result metadataIntegrationResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
	return result
}
