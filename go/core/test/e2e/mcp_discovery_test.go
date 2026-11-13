package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestMCPClassificationFailurePreservesHealthyToolsAndRecovers(t *testing.T) {
	// Given healthy and unavailable catalogs, when an execution continues and
	// another execution resumes, assert that failed tools stay hidden until recovery.
	target := interactionTarget(t)
	var unavailable atomic.Bool
	unavailable.Store(true)
	var runtimeLists, healthyCalls, recoveredCalls, hiddenCalls atomic.Int32
	healthy := newMCPDiscoveryServer()
	mcpsdk.AddTool(healthy, &mcpsdk.Tool{Name: "healthy_tool"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		healthyCalls.Add(1)
		// Recovery between model steps must not undo this execution's omission.
		unavailable.Store(false)
		return nil, map[string]any{"result": "healthy"}, nil
	})
	recovering := newMCPDiscoveryServer()
	mcpsdk.AddTool(recovering, &mcpsdk.Tool{Name: "recovered_tool"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		recoveredCalls.Add(1)
		return nil, map[string]any{"result": "recovered"}, nil
	})
	mcpsdk.AddTool(recovering, &mcpsdk.Tool{Name: "app_only_tool", Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}}}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		hiddenCalls.Add(1)
		return nil, map[string]any{"result": "hidden"}, nil
	})
	recovering.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/list" && req.GetSession().(*mcpsdk.ServerSession).InitializeParams().ClientInfo.Name == "kagent-adk" {
				runtimeLists.Add(1)
				if unavailable.Load() {
					return nil, errors.New("runtime catalog temporarily unavailable")
				}
			}
			return next(ctx, method, req)
		}
	})
	var modelRequests atomic.Int32
	modelURL := startMCPDiscoveryModel(t, func(request discoveryModelRequest) (string, string, error) {
		modelRequests.Add(1)
		recovered := strings.Contains(request.lastUser(), "recovered")
		names := request.toolNames()
		if !slices.Contains(names, "healthy_tool") || slices.Contains(names, "app_only_tool") || slices.Contains(names, "recovered_tool") != recovered {
			return "", "", fmt.Errorf("unexpected tool visibility for recovered=%t: %v", recovered, names)
		}
		toolName, result := "healthy_tool", "HEALTHY_DISCOVERY_OK"
		if recovered {
			toolName, result = "recovered_tool", "RECOVERED_DISCOVERY_OK"
		}
		if _, ok := request.toolResults(true)[toolName]; !ok {
			return toolName, "", nil
		}
		return "", result, nil
	})
	template := createMCPDiscoveryTemplate(t, modelURL, startMCPDiscoveryServer(t, healthy), startMCPDiscoveryServer(t, recovering))
	require.Zero(t, runtimeLists.Load(), "runtime startup must not classify remote tools")
	fixture := newInteractionFixtureForTemplate(t, testHarness{name: "kagent"}, target, template)
	_, _, first := fixture.send(t, "Use the healthy tool.")
	require.Equal(t, a2atype.TaskStateCompleted, first.Status.State, taskText(first))
	require.Contains(t, taskText(first), "HEALTHY_DISCOVERY_OK")
	require.EqualValues(t, 1, healthyCalls.Load())
	require.Zero(t, recoveredCalls.Load())
	require.EqualValues(t, 1, runtimeLists.Load(), "an omitted catalog must not be rediscovered in later model steps")

	assertActorSuspended(t, fixture)
	_, _, second := fixture.send(t, "Use the recovered tool.")
	require.Equal(t, a2atype.TaskStateCompleted, second.Status.State, taskText(second))
	require.Contains(t, taskText(second), "RECOVERED_DISCOVERY_OK")
	require.EqualValues(t, 1, recoveredCalls.Load())
	require.EqualValues(t, 1, healthyCalls.Load())
	require.Zero(t, hiddenCalls.Load())
	require.Greater(t, runtimeLists.Load(), int32(1))
	require.EqualValues(t, 4, modelRequests.Load(), "visibility must be checked before and after both tool calls")
}

func TestMCPAppCompactionPreservesHistoryAcrossResume(t *testing.T) {
	// Given an app tool and an ordinary tool, when a restored execution prepares
	// model history, assert that only app results are compacted and public history stays rich.
	target := interactionTarget(t)
	renderPayload := map[string]any{"panel": map[string]any{"title": "Discovery test panel", "rows": []any{"RICH_MCP_RENDER_PAYLOAD", strings.Repeat("render data ", 128)}}}
	ordinaryPayload := map[string]any{"note": "ORDINARY_MCP_RESULT"}
	var appCalls, ordinaryCalls, hiddenCalls, runtimeLists atomic.Int32
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "cached-app-discovery", Version: "1"}, &mcpsdk.ServerOptions{
		SupportedProtocolVersions: []string{"2026-07-28"},
		SetCacheable:              func(_ context.Context, _ mcpsdk.Request, c *mcpsdk.Cacheable) { c.TTLMs = 60000 },
	})
	server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/list" && req.(*mcpsdk.ListToolsRequest).ClientInfo().Name == "kagent-adk" {
				runtimeLists.Add(1)
			}
			return next(ctx, method, req)
		}
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "show_panel", Meta: mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://discovery/panel"}}}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		appCalls.Add(1)
		return nil, renderPayload, nil
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "ordinary_tool"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		ordinaryCalls.Add(1)
		return nil, ordinaryPayload, nil
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "app_only_tool", Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}}}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		hiddenCalls.Add(1)
		return nil, map[string]any{"result": "hidden"}, nil
	})
	var restoredRequests atomic.Int32
	modelURL := startMCPDiscoveryModel(t, func(request discoveryModelRequest) (string, string, error) {
		names := request.toolNames()
		if !slices.Contains(names, "show_panel") || !slices.Contains(names, "ordinary_tool") || slices.Contains(names, "app_only_tool") {
			return "", "", fmt.Errorf("unexpected model tools: %v", names)
		}
		results := request.toolResults(false)
		appResult, rendered := results["show_panel"]
		if !rendered {
			return "show_panel", "", nil
		}
		if appResult != "The interactive UI for this tool has been rendered to the user and now updates live inside the app. Treat this as complete and do not call this tool again unless the user explicitly asks for it." {
			return "", "", fmt.Errorf("app result was not compacted: %s", appResult)
		}
		ordinaryResult, called := results["ordinary_tool"]
		if !called {
			return "ordinary_tool", "", nil
		}
		var output struct {
			Output map[string]any `json:"output"`
		}
		if err := json.Unmarshal([]byte(ordinaryResult), &output); err != nil || len(output.Output) != len(ordinaryPayload) || output.Output["note"] != ordinaryPayload["note"] {
			return "", "", fmt.Errorf("ordinary result was modified: %s", ordinaryResult)
		}
		if strings.Contains(request.lastUser(), "without calling tools") {
			restoredRequests.Add(1)
			return "", "MCP_APP_HISTORY_RESTORED", nil
		}
		return "", "MCP_APP_RENDERED", nil
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: true})
	serverURL := reachableServerURL(t, startMCPDiscoveryHTTP(t, handler), "/mcp")
	template := createMCPDiscoveryTemplate(t, modelURL, serverURL)
	fixture := newInteractionFixtureForTemplate(t, testHarness{name: "kagent"}, target, template)
	_, _, first := fixture.send(t, "Render the panel and call the ordinary tool.")
	require.Equal(t, a2atype.TaskStateCompleted, first.Status.State, taskText(first))
	require.Contains(t, taskText(first), "MCP_APP_RENDERED")
	require.EqualValues(t, 1, runtimeLists.Load(), "SDK caching must preserve filtering and app metadata across model steps")
	assertPersistedMCPResult(t, fixture, first.ID, "show_panel", renderPayload)
	assertPersistedMCPResult(t, fixture, first.ID, "ordinary_tool", ordinaryPayload)

	assertActorSuspended(t, fixture)
	_, _, second := fixture.send(t, "Summarize the previous results without calling tools.")
	require.Equal(t, a2atype.TaskStateCompleted, second.Status.State, taskText(second))
	require.Contains(t, taskText(second), "MCP_APP_HISTORY_RESTORED")
	require.EqualValues(t, 1, restoredRequests.Load(), "the first restored model request must already contain compacted history")
	require.EqualValues(t, 1, appCalls.Load(), "restoring history must not invoke the app again")
	require.EqualValues(t, 1, ordinaryCalls.Load())
	require.Zero(t, hiddenCalls.Load())
	require.EqualValues(t, 2, runtimeLists.Load(), "a restored process must discover once after readiness")
	assertPersistedMCPResult(t, fixture, first.ID, "show_panel", renderPayload)
	assertPersistedMCPResult(t, fixture, first.ID, "ordinary_tool", ordinaryPayload)
}

func TestSlowMCPDiscoveryDoesNotBlockResume(t *testing.T) {
	for _, slow := range []string{"tools/list", "GET"} {
		t.Run("Given slow "+strings.ReplaceAll(slow, "/", " ")+", when an agent becomes ready and resumes repeatedly, assert that discovery waits until invocation", func(t *testing.T) {
			target := interactionTarget(t)
			harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
			url, runtimeDiscovery := startSlowMCPServer(t, slow)
			template, _ := createMCPInteractionTemplate(t, harness, url, false, nil)
			require.Zero(t, runtimeDiscovery.Load(), "golden runtime readiness must not depend on MCP discovery")
			fixture := newInteractionFixtureForTemplate(t, harness, target, template)
			for turn := range 2 {
				if turn > 0 {
					assertActorSuspended(t, fixture)
				}
				// Use one SendMessage call: retries would hide a parking timeout.
				_, request := newMessageRequest(t, "Add 3 and 5 using the configured MCP server.")
				request.Message.ContextId = fixture.sessionID
				request.Tenant = fixture.tenant
				response, err := fixture.client.SendMessage(fixture.ctx, request)
				require.NoError(t, err, "slow MCP discovery must not make the router reject a restore")
				result, err := pbconv.FromProtoSendMessageResponse(response)
				require.NoError(t, err)
				task, ok := result.(*a2atype.Task)
				require.True(t, ok, "A2A response = %T, want Task", result)
				require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
				require.Contains(t, taskText(task), "result is 8")
			}
			require.Positive(t, runtimeDiscovery.Load(), "successful invocations must actually discover the slow server")
		})
	}
}

func startSlowMCPServer(t *testing.T, slow string) (string, *atomic.Int32) {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "slow-startup", Version: "1"}, &mcpsdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "add_numbers", Description: "Add two numbers"}, func(_ context.Context, _ *mcpsdk.CallToolRequest, input struct {
		A int `json:"a"`
		B int `json:"b"`
	}) (*mcpsdk.CallToolResult, map[string]any, error) {
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "The result is 8."}}}, map[string]any{"result": input.A + input.B}, nil
	})
	var clients sync.Map
	runtimeDiscovery := new(atomic.Int32)
	server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			session := req.GetSession().(*mcpsdk.ServerSession)
			if method == "initialize" {
				clients.Store(session.ID(), req.GetParams().(*mcpsdk.InitializeParams).ClientInfo.Name)
			}
			if method == "tools/list" && session.InitializeParams().ClientInfo.Name == "kagent-adk" {
				runtimeDiscovery.Add(1)
				if slow == method {
					if err := waitForSlowDiscovery(ctx); err != nil {
						return nil, err
					}
				}
			}
			return next(ctx, method, req)
		}
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	baseURL := startMCPDiscoveryHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if client, ok := clients.Load(r.Header.Get("Mcp-Session-Id")); ok && client == "kagent-adk" {
				runtimeDiscovery.Add(1)
				if slow == r.Method {
					if err := waitForSlowDiscovery(r.Context()); err != nil {
						return
					}
				}
			}
		}
		handler.ServeHTTP(w, r)
	}))
	return reachableServerURL(t, baseURL, "/mcp"), runtimeDiscovery
}

func waitForSlowDiscovery(ctx context.Context) error {
	// Longer than the E2E router's five-second parking budget, while fitting
	// within the configured MCP request timeout. Workload requests may take longer.
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func newMCPDiscoveryServer() *mcpsdk.Server {
	return mcpsdk.NewServer(&mcpsdk.Implementation{Name: "discovery-e2e", Version: "1"}, &mcpsdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
}

func startMCPDiscoveryServer(t *testing.T, server *mcpsdk.Server) string {
	t.Helper()
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	return reachableServerURL(t, startMCPDiscoveryHTTP(t, handler), "/mcp")
}

func startMCPDiscoveryHTTP(t *testing.T, handler http.Handler) string {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	require.NoError(t, server.Listener.Close())
	var err error
	server.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Start()
	t.Cleanup(func() { server.CloseClientConnections(); server.Close() })
	return server.URL
}

func createMCPDiscoveryTemplate(t *testing.T, modelURL string, serverURLs ...string) string {
	t.Helper()
	kube := interactionKubeClient(t)
	harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
	model := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "mcp-discovery-", Namespace: "kagent"},
		Spec:       v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4.1-mini", Stream: new(false), OpenAI: &v1alpha3.OpenAIConfig{BaseURL: modelURL}},
	}
	require.NoError(t, kube.Create(t.Context(), model))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), model); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete discovery model: %v", err)
		}
	})
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "mcp-discovery-", Namespace: "kagent", Labels: harness.labels()},
		Spec:       v1alpha3.AgentTemplateSpec{Description: "MCP discovery E2E fixture", SystemPrompt: "Use the configured MCP tools.", ModelConfig: &v1alpha3.ModelConfigReference{Name: model.Name}},
	}
	for _, serverURL := range serverURLs {
		server := &v1alpha3.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "mcp-discovery-", Namespace: "kagent"},
			Spec:       v1alpha3.RemoteMCPServerSpec{Description: "MCP discovery E2E fixture", Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp, URL: serverURL},
		}
		require.NoError(t, kube.Create(t.Context(), server))
		t.Cleanup(func() {
			if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("delete MCP server: %v", err)
			}
		})
		template.Spec.Tools = append(template.Spec.Tools, v1alpha3.ToolBinding{MCP: &v1alpha3.MCPToolBinding{Server: v1alpha3.MCPServerReference{Kind: new("RemoteMCPServer"), Name: server.Name}}})
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.name)
	return template.Name
}

func assertPersistedMCPResult(t *testing.T, fixture *interactionFixture, taskID a2atype.TaskID, toolName string, payload map[string]any) {
	t.Helper()
	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{Tenant: fixture.tenant, ID: taskID})
	require.NoError(t, err)
	response, err := fixture.client.GetTask(fixture.ctx, request)
	require.NoError(t, err)
	task, err := pbconv.FromProtoTask(response)
	require.NoError(t, err)
	var responses []any
	// The runtime publishes tool results as A2A artifacts. These are the
	// persisted chat timeline payloads; Task.History contains input messages.
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			activity, ok := kagenta2a.ParseToolActivity(part)
			if ok && activity.Kind == kagenta2a.ToolResultKind && activity.Name == toolName {
				responses = append(responses, activity.Response)
			}
		}
	}
	require.Len(t, responses, 1, "persisted artifacts must contain the original %s result", toolName)
	require.Equal(t, map[string]any{"output": payload}, responses[0])
}

type discoveryModelRequest struct {
	Messages []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

func (r discoveryModelRequest) lastUser() string {
	for _, message := range slices.Backward(r.Messages) {
		if message.Role == "user" {
			return message.Content
		}
	}
	return ""
}

func (r discoveryModelRequest) toolNames() []string {
	var names []string
	for _, tool := range r.Tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func (r discoveryModelRequest) toolResults(currentTurnOnly bool) map[string]string {
	names := make(map[string]string)
	start := 0
	for i, message := range r.Messages {
		for _, call := range message.ToolCalls {
			names[call.ID] = call.Function.Name
		}
		if currentTurnOnly && message.Role == "user" {
			start = i
		}
	}
	results := make(map[string]string)
	for _, message := range r.Messages[start:] {
		if message.Role == "tool" {
			results[names[message.ToolCallID]] = message.Content
		}
	}
	return results
}

func startMCPDiscoveryModel(t *testing.T, respond func(discoveryModelRequest) (toolName, text string, err error)) string {
	t.Helper()
	baseURL := startMCPDiscoveryHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request discoveryModelRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Logf("model mock could not decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		toolName, text, err := respond(request)
		if err != nil {
			t.Logf("model mock rejected request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		message := map[string]any{"role": "assistant", "content": text}
		finishReason := "stop"
		if toolName != "" {
			message["tool_calls"] = []any{map[string]any{"id": uuid.NewString(), "type": "function", "function": map[string]any{"name": toolName, "arguments": "{}"}}}
			finishReason = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"id": uuid.NewString(), "object": "chat.completion", "model": "test-model", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason}}}); err != nil {
			t.Errorf("write model response: %v", err)
		}
	}))
	return reachableModelURL(t, baseURL)
}
