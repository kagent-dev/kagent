package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

func TestRealDSHThroughA2AGRPCAndMCP(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("set DSH_TEST_BIN to the pinned DSH executable")
	}
	var toolCalls atomic.Int32
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "arithmetic", Version: "1"}, nil)
	type operands struct {
		A int `json:"a"`
		B int `json:"b"`
	}
	type product struct {
		Product int `json:"product"`
	}
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "multiply", Description: "Multiply two integers."}, func(_ context.Context, _ *mcp.CallToolRequest, input operands) (*mcp.CallToolResult, product, error) {
		toolCalls.Add(1)
		return nil, product{Product: input.A * input.B}, nil
	})
	mcpHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer mcpHTTP.Close()
	var mu sync.Mutex
	var requests []json.RawMessage
	modelHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		mu.Lock()
		requests = append(requests, body)
		index := len(requests)
		mu.Unlock()
		if index == 1 {
			var req struct {
				Tools []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tools"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, "bad tools", 400)
				return
			}
			name := ""
			for _, tool := range req.Tools {
				if strings.HasSuffix(tool.Function.Name, "multiply") {
					name = tool.Function.Name
				}
			}
			if name == "" {
				http.Error(w, "MCP tool not registered", 500)
				return
			}
			writeCompletion(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "multiply-1", "type": "function", "function": map[string]string{"name": name, "arguments": `{"a":6,"b":7}`}}}}, "tool_calls")
		} else {
			text := "product=42"
			if index == 3 {
				text = "resumed product=42"
			}
			writeCompletion(w, map[string]string{"role": "assistant", "content": text}, "stop")
		}
	}))
	defer modelHTTP.Close()
	data := fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"instruction":"Use arithmetic tools.","runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q}}]}`, modelHTTP.URL+"/v1", mcpHTTP.URL)
	cfg, err := config.Parse([]byte(data))
	require.NoError(t, err)
	dataDir := t.TempDir()
	runner, err := New(cfg, executable, dataDir, append(os.Environ(), "OPENAI_API_KEY=mock"))
	require.NoError(t, err)
	store, err := continuation.New(dataDir+"/adapter", "dsh-byo", ValidateSessionID)
	require.NoError(t, err)
	executor, err := runtimea2a.New(runner, store, tracing.RuntimeTelemetry{})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	a2agrpc.NewHandler(a2asrv.NewHandler(transportFixture{executor})).RegisterWith(server)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, <-serveDone) })
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := a2apb.NewA2AServiceClient(connection)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	contextID := ""
	for i, prompt := range []string{"Calculate six times seven.", "Continue the calculation."} {
		message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(prompt))
		message.ContextID = contextID
		request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: message})
		require.NoError(t, err)
		stream, err := client.SendStreamingMessage(ctx, request)
		require.NoError(t, err)
		var text strings.Builder
		completed := false
		for {
			response, err := stream.Recv()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			event, err := pbconv.FromProtoStreamResponse(response)
			require.NoError(t, err)
			switch event := event.(type) {
			case *a2atype.Task:
				contextID = event.ContextID
			case *a2atype.TaskArtifactUpdateEvent:
				for _, part := range event.Artifact.Parts {
					text.WriteString(part.Text())
				}
			case *a2atype.TaskStatusUpdateEvent:
				if event.Status.State == a2atype.TaskStateCompleted {
					completed = true
				}
				require.NotEqual(t, a2atype.TaskStateFailed, event.Status.State)
			}
		}
		require.True(t, completed)
		want := "product=42"
		if i == 1 {
			want = "resumed product=42"
		}
		require.Equal(t, want, text.String())
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, int32(1), toolCalls.Load())
	require.Len(t, requests, 3)
	require.Contains(t, string(requests[1]), "42")
	require.Contains(t, string(requests[2]), "product=42")
}

// The production App's controller-backed TaskStore publishes the initial task.
// This transport-only fixture substitutes an SDK-local task for that boundary.
type transportFixture struct{ a2asrv.AgentExecutor }

func (f transportFixture) Execute(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if input.TaskID == "" {
			input.TaskID = a2atype.NewTaskID()
		}
		if input.ContextID == "" {
			input.ContextID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		}
		if !yield(a2atype.NewSubmittedTask(input, input.Message), nil) {
			return
		}
		for event, err := range f.AgentExecutor.Execute(ctx, input) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func writeCompletion(w http.ResponseWriter, delta any, finish string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, choice := range []any{map[string]any{"delta": delta, "index": 0, "finish_reason": nil}, map[string]any{"delta": map[string]any{}, "index": 0, "finish_reason": finish}} {
		data, _ := json.Marshal(map[string]any{"id": "chat-test", "object": "chat.completion.chunk", "model": "mock", "choices": []any{choice}})
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}
