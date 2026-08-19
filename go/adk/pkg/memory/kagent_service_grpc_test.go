package memory

import (
	"context"
	"encoding/json"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	"github.com/kagent-dev/kagent/go/adk/pkg/embedding"
	"github.com/kagent-dev/kagent/go/api/adk"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/memory"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type memoryTestServer struct {
	apiv1alpha1.UnimplementedMemoryServiceServer
	add    func(context.Context, *apiv1alpha1.MemoryServiceAddSessionRequest) (*apiv1alpha1.MemoryServiceAddSessionResponse, error)
	search func(context.Context, *apiv1alpha1.MemoryServiceSearchRequest) (*apiv1alpha1.MemoryServiceSearchResponse, error)
}

func (server *memoryTestServer) AddSession(ctx context.Context, request *apiv1alpha1.MemoryServiceAddSessionRequest) (*apiv1alpha1.MemoryServiceAddSessionResponse, error) {
	return server.add(ctx, request)
}

func (server *memoryTestServer) Search(ctx context.Context, request *apiv1alpha1.MemoryServiceSearchRequest) (*apiv1alpha1.MemoryServiceSearchResponse, error) {
	return server.search(ctx, request)
}

func newMemoryControllerClient(t *testing.T, service *memoryTestServer) *controllerclient.Client {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apiv1alpha1.RegisterMemoryServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()

	client, err := controllerclient.New(controllerclient.Config{
		Target: "passthrough:///bufnet",
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		server.Stop()
		require.NoError(t, listener.Close())
	})
	return client
}

func newMockEmbeddingClient(t *testing.T) (*embedding.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		vector := make([]float64, 768)
		vector[0] = 1
		response.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(response).Encode(map[string]any{
			"data":  []map[string]any{{"embedding": vector, "index": 0}},
			"model": "test",
		}))
	}))
	client, err := embedding.New(embedding.Config{EmbeddingConfig: &adk.EmbeddingConfig{
		Provider: "openai",
		Model:    "test-model",
		BaseUrl:  server.URL + "/v1",
	}})
	require.NoError(t, err)
	return client, server
}

func TestKagentMemoryServiceAddSessionUsesGRPC(t *testing.T) {
	tests := []struct {
		name         string
		session      adksession.Session
		wantRequests int
		rpcError     bool
	}{
		{name: "empty session", session: newMockSession("session", "user", nil)},
		{
			name: "single message",
			session: newMockSession("session", "user", []*adksession.Event{
				newMockEvent("user", "Hello, how are you?"),
			}),
			wantRequests: 1,
		},
		{
			name: "multiple messages",
			session: newMockSession("session", "user", []*adksession.Event{
				newMockEvent("user", "What is the weather?"),
				newMockEvent("agent", "The weather is sunny."),
			}),
			wantRequests: 1,
		},
		{
			name: "server error",
			session: newMockSession("session", "user", []*adksession.Event{
				newMockEvent("user", "Hello"),
			}),
			wantRequests: 1,
			rpcError:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestCount := 0
			controllerClient := newMemoryControllerClient(t, &memoryTestServer{add: func(ctx context.Context, request *apiv1alpha1.MemoryServiceAddSessionRequest) (*apiv1alpha1.MemoryServiceAddSessionResponse, error) {
				requestCount++
				input := request.GetMemory()
				assert.Equal(t, "test-agent", input.GetAgentName())
				assert.Equal(t, "user", input.GetUserId())
				assert.NotEmpty(t, input.GetContent())
				assert.Len(t, input.GetVector(), 768)
				assert.Equal(t, int32(15), input.GetTtlDays())
				values, _ := metadata.FromIncomingContext(ctx)
				assert.Equal(t, []string{"user"}, values.Get("x-user-id"))
				if test.rpcError {
					return nil, status.Error(codes.Internal, "store failed")
				}
				return &apiv1alpha1.MemoryServiceAddSessionResponse{Id: "memory-1"}, nil
			}})
			embeddingClient, embeddingServer := newMockEmbeddingClient(t)
			defer embeddingServer.Close()
			service := &KagentMemoryService{
				agentName:        "test-agent",
				controllerClient: controllerClient,
				ttlDays:          15,
				embeddingClient:  embeddingClient,
			}

			err := service.AddSessionToMemory(t.Context(), test.session)
			if test.rpcError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.wantRequests, requestCount)
		})
	}
}

func TestKagentMemoryServiceSearchUsesGRPC(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		results      []*apiv1alpha1.MemorySearchResult
		wantContents []string
	}{
		{name: "empty query"},
		{
			name:  "successful search",
			query: "weather",
			results: []*apiv1alpha1.MemorySearchResult{
				{Id: "memory-1", Content: "The weather is sunny", Score: 0.9},
				{Id: "memory-2", Content: "Weather forecast for tomorrow", Score: 0.7},
			},
			wantContents: []string{"The weather is sunny", "Weather forecast for tomorrow"},
		},
		{name: "no results", query: "unknown", results: []*apiv1alpha1.MemorySearchResult{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestCount := 0
			controllerClient := newMemoryControllerClient(t, &memoryTestServer{search: func(ctx context.Context, request *apiv1alpha1.MemoryServiceSearchRequest) (*apiv1alpha1.MemoryServiceSearchResponse, error) {
				requestCount++
				assert.Equal(t, "test-agent", request.GetAgentName())
				assert.Equal(t, "user", request.GetUserId())
				assert.Len(t, request.GetVector(), 768)
				assert.Equal(t, int32(5), request.GetLimit())
				assert.InDelta(t, 0.3, request.GetMinScore(), 0.0001)
				values, _ := metadata.FromIncomingContext(ctx)
				assert.Equal(t, []string{"user"}, values.Get("x-user-id"))
				return &apiv1alpha1.MemoryServiceSearchResponse{Memories: test.results}, nil
			}})
			embeddingClient, embeddingServer := newMockEmbeddingClient(t)
			defer embeddingServer.Close()
			service := &KagentMemoryService{
				agentName:        "test-agent",
				controllerClient: controllerClient,
				embeddingClient:  embeddingClient,
			}

			response, err := service.SearchMemory(t.Context(), &memory.SearchRequest{Query: test.query, UserID: "user"})
			require.NoError(t, err)
			require.Len(t, response.Memories, len(test.wantContents))
			for index, content := range test.wantContents {
				require.NotNil(t, response.Memories[index].Content)
				assert.Equal(t, "user", response.Memories[index].Content.Role)
				assert.Equal(t, content, response.Memories[index].Content.Parts[0].Text)
			}
			if test.query == "" {
				assert.Zero(t, requestCount)
			} else {
				assert.Equal(t, 1, requestCount)
			}
		})
	}
}

func TestStoreMemoryPreservesTTLAndReturnsRPCError(t *testing.T) {
	tests := []struct {
		name    string
		ttlDays int
		rpcErr  error
	}{
		{name: "server default TTL"},
		{name: "explicit TTL", ttlDays: 15},
		{name: "RPC error", ttlDays: 15, rpcErr: status.Error(codes.InvalidArgument, "bad memory")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controllerClient := newMemoryControllerClient(t, &memoryTestServer{add: func(_ context.Context, request *apiv1alpha1.MemoryServiceAddSessionRequest) (*apiv1alpha1.MemoryServiceAddSessionResponse, error) {
				if test.ttlDays == 0 {
					assert.Nil(t, request.GetMemory().TtlDays)
				} else {
					assert.Equal(t, int32(test.ttlDays), request.GetMemory().GetTtlDays())
				}
				return &apiv1alpha1.MemoryServiceAddSessionResponse{Id: "memory-1"}, test.rpcErr
			}})
			service := &KagentMemoryService{
				agentName:        "test-agent",
				controllerClient: controllerClient,
				ttlDays:          test.ttlDays,
			}
			err := service.storeMemory(t.Context(), "user", "content", make([]float32, 768))
			if test.rpcErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKagentMemoryServiceExtractSessionContent(t *testing.T) {
	tests := []struct {
		name        string
		events      []*adksession.Event
		wantContent string
	}{
		{name: "no events"},
		{
			name: "events with text",
			events: []*adksession.Event{
				newMockEvent("user", "Hello"),
				newMockEvent("agent", "Hi there!"),
			},
			wantContent: "user: Hello",
		},
		{
			name:   "function call only",
			events: []*adksession.Event{newMockEventWithFunctionCall("agent", "get_weather")},
		},
		{
			name: "function response is extracted",
			events: []*adksession.Event{
				newMockEventWithFunctionResponse("agent", "get_weather", map[string]any{"temperature": 72, "condition": "sunny"}),
			},
			wantContent: `"condition":"sunny"`,
		},
		{
			name: "long function response is truncated",
			events: []*adksession.Event{
				newMockEventWithFunctionResponse("agent", "get_weather", map[string]any{
					"forecast": strings.Repeat("x", 1000),
				}),
			},
			wantContent: "...(truncated)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &KagentMemoryService{agentName: "test-agent"}
			content := service.extractSessionContent(newMockSession("session", "user", test.events))
			if test.wantContent == "" {
				assert.Empty(t, content)
			} else {
				assert.True(t, strings.Contains(content, test.wantContent))
			}
		})
	}
}

func TestNew(t *testing.T) {
	validEmbedding := &adk.EmbeddingConfig{Provider: "openai", Model: "text-embedding-3-small"}
	controllerClient := &controllerclient.Client{}
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name: "valid config",
			config: Config{
				AgentName:        "test-agent",
				ControllerClient: controllerClient,
				EmbeddingConfig:  validEmbedding,
			},
		},
		{name: "missing agent name", config: Config{ControllerClient: controllerClient, EmbeddingConfig: validEmbedding}, wantErr: "agent name is required"},
		{name: "missing controller client", config: Config{AgentName: "test-agent", EmbeddingConfig: validEmbedding}, wantErr: "controller client is required"},
		{name: "missing embedding config", config: Config{AgentName: "test-agent", ControllerClient: controllerClient}, wantErr: "embedding config is required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := New(test.config)
			if test.wantErr != "" {
				require.EqualError(t, err, test.wantErr)
				assert.Nil(t, service)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.config.AgentName, service.agentName)
		})
	}
}

type mockSession struct {
	id      string
	userID  string
	appName string
	events  *mockEvents
}

func newMockSession(id, userID string, events []*adksession.Event) *mockSession {
	return &mockSession{id: id, userID: userID, appName: "test-app", events: &mockEvents{events: events}}
}

func (session *mockSession) ID() string                { return session.id }
func (session *mockSession) UserID() string            { return session.userID }
func (session *mockSession) AppName() string           { return session.appName }
func (session *mockSession) State() adksession.State   { return nil }
func (session *mockSession) Events() adksession.Events { return session.events }
func (session *mockSession) LastUpdateTime() time.Time { return time.Now() }

type mockEvents struct {
	events []*adksession.Event
}

func (events *mockEvents) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		for _, event := range events.events {
			if !yield(event) {
				return
			}
		}
	}
}

func (events *mockEvents) Len() int {
	return len(events.events)
}

func (events *mockEvents) At(index int) *adksession.Event {
	if index < 0 || index >= len(events.events) {
		return nil
	}
	return events.events[index]
}

func newMockEvent(author, text string) *adksession.Event {
	event := &adksession.Event{
		ID:           "event-" + author,
		Author:       author,
		Timestamp:    time.Now(),
		InvocationID: "invocation-1",
		Actions:      adksession.EventActions{StateDelta: make(map[string]any)},
	}
	event.Content = &genai.Content{Role: author, Parts: []*genai.Part{{Text: text}}}
	return event
}

func newMockEventWithFunctionCall(author, functionName string) *adksession.Event {
	event := newMockEvent(author, "")
	event.Content = &genai.Content{Role: author, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: functionName}}}}
	return event
}

func newMockEventWithFunctionResponse(author, functionName string, response map[string]any) *adksession.Event {
	event := newMockEvent(author, "")
	event.Content = &genai.Content{
		Role: author,
		Parts: []*genai.Part{{
			FunctionResponse: &genai.FunctionResponse{Name: functionName, Response: response},
		}},
	}
	return event
}
