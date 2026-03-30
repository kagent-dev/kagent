package memory

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/embedding"
	"github.com/kagent-dev/kagent/go/api/adk"
	"google.golang.org/adk/memory"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// newMockEmbeddingClient creates a mock embedding client backed by a test HTTP server
// that returns a fixed non-zero vector for any input.
func newMockEmbeddingClient(t *testing.T) (*embedding.Client, *httptest.Server) {
	t.Helper()
	embServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vec := make([]float64, 768)
		vec[0] = 1.0
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"embedding": vec, "index": 0}},
			"model": "test",
		})
	}))
	client, err := embedding.New(embedding.Config{
		EmbeddingConfig: &adk.EmbeddingConfig{
			Provider: "openai",
			Model:    "test-model",
			BaseUrl:  embServer.URL + "/v1",
		},
		HTTPClient: embServer.Client(),
	})
	if err != nil {
		t.Fatalf("failed to create mock embedding client: %v", err)
	}
	return client, embServer
}

func TestKagentMemoryService_AddSession(t *testing.T) {
	tests := []struct {
		name           string
		session        adksession.Session
		wantRequests   int
		wantStatusCode int
		wantErr        bool
	}{
		{
			name:           "empty_session_no_content",
			session:        newMockSession("sess1", "user1", nil),
			wantRequests:   0,
			wantStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name: "single_user_message",
			session: newMockSession("sess1", "user1", []*adksession.Event{
				newMockEvent("user", "Hello, how are you?"),
			}),
			wantRequests:   1,
			wantStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name: "multiple_messages",
			session: newMockSession("sess1", "user1", []*adksession.Event{
				newMockEvent("user", "What is the weather?"),
				newMockEvent("agent", "The weather is sunny."),
				newMockEvent("user", "Thank you!"),
			}),
			wantRequests:   1,
			wantStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name: "api_error",
			session: newMockSession("sess1", "user1", []*adksession.Event{
				newMockEvent("user", "Hello"),
			}),
			wantRequests:   1,
			wantStatusCode: http.StatusInternalServerError,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++

				// Verify request structure
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				if r.URL.Path != "/api/memories/sessions" {
					t.Errorf("Expected /api/memories/sessions, got %s", r.URL.Path)
				}

				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				// Decode and verify request body
				var req addSessionRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode request: %v", err)
				}

				if req.AgentName != "test-agent" {
					t.Errorf("Expected agent_name test-agent, got %s", req.AgentName)
				}

				if req.UserID != "user1" {
					t.Errorf("Expected user_id user1, got %s", req.UserID)
				}

				if req.Content == "" {
					t.Errorf("Expected non-empty content")
				}

				if len(req.Vector) != 768 {
					t.Errorf("Expected 768-dimensional vector, got %d", len(req.Vector))
				}

				w.WriteHeader(tt.wantStatusCode)
			}))
			defer server.Close()

			embClient, embServer := newMockEmbeddingClient(t)
			defer embServer.Close()

			svc := &KagentMemoryService{
				agentName:       "test-agent",
				apiURL:          server.URL,
				client:          server.Client(),
				ttlDays:         15,
				embeddingClient: embClient,
				model:           nil, // No summarization
			}

			err := svc.AddSession(context.Background(), tt.session)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddSession() error = %v, wantErr %v", err, tt.wantErr)
			}

			if requestCount != tt.wantRequests {
				t.Errorf("Expected %d requests, got %d", tt.wantRequests, requestCount)
			}
		})
	}
}

func TestKagentMemoryService_Search(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		userID         string
		serverResponse []searchResultItem
		wantCount      int
		wantErr        bool
	}{
		{
			name:           "empty_query",
			query:          "",
			userID:         "user1",
			serverResponse: nil,
			wantCount:      0,
			wantErr:        false,
		},
		{
			name:   "successful_search",
			query:  "weather",
			userID: "user1",
			serverResponse: []searchResultItem{
				{ID: "mem1", Content: "The weather is sunny", Score: 0.9},
				{ID: "mem2", Content: "Weather forecast for tomorrow", Score: 0.7},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:           "no_results",
			query:          "xyz",
			userID:         "user1",
			serverResponse: []searchResultItem{},
			wantCount:      0,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				if r.URL.Path != "/api/memories/search" {
					t.Errorf("Expected /api/memories/search, got %s", r.URL.Path)
				}

				// Decode and verify search request
				var req searchRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode request: %v", err)
				}

				if req.AgentName != "test-agent" {
					t.Errorf("Expected agent_name test-agent, got %s", req.AgentName)
				}

				if req.UserID != tt.userID {
					t.Errorf("Expected user_id %s, got %s", tt.userID, req.UserID)
				}

				if len(req.Vector) != 768 {
					t.Errorf("Expected 768-dimensional vector, got %d", len(req.Vector))
				}

				if req.Limit != 5 {
					t.Errorf("Expected limit 5, got %d", req.Limit)
				}

				if req.MinScore != 0.3 {
					t.Errorf("Expected min_score 0.3, got %f", req.MinScore)
				}

				// Return mock results
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			embClient, embServer := newMockEmbeddingClient(t)
			defer embServer.Close()

			svc := &KagentMemoryService{
				agentName:       "test-agent",
				apiURL:          server.URL,
				client:          server.Client(),
				ttlDays:         15,
				embeddingClient: embClient,
				model:           nil,
			}

			resp, err := svc.Search(context.Background(), &memory.SearchRequest{
				Query:  tt.query,
				UserID: tt.userID,
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("Search() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && resp != nil {
				if len(resp.Memories) != tt.wantCount {
					t.Errorf("Expected %d memories, got %d", tt.wantCount, len(resp.Memories))
				}

				// Verify memory structure
				for i, mem := range resp.Memories {
					if mem.Content == nil {
						t.Errorf("Memory %d has nil Content", i)
					}
					if mem.Content.Role != "user" {
						t.Errorf("Memory %d expected role 'user', got '%s'", i, mem.Content.Role)
					}
					if len(mem.Content.Parts) == 0 {
						t.Errorf("Memory %d has no parts", i)
					}
				}
			}
		})
	}
}

func TestKagentMemoryService_StoreMemory(t *testing.T) {
	tests := []struct {
		name           string
		userID         string
		content        string
		vectorDim      int
		wantStatusCode int
		wantErr        bool
	}{
		{
			name:           "valid_request",
			userID:         "user1",
			content:        "Test memory",
			vectorDim:      768,
			wantStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name:           "server_error",
			userID:         "user1",
			content:        "Test memory",
			vectorDim:      768,
			wantStatusCode: http.StatusInternalServerError,
			wantErr:        true,
		},
		{
			name:           "bad_request",
			userID:         "user1",
			content:        "Test memory",
			vectorDim:      768,
			wantStatusCode: http.StatusBadRequest,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req addSessionRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode request: %v", err)
				}

				// Verify structure
				if req.AgentName == "" {
					t.Error("Missing agent_name")
				}
				if req.UserID == "" {
					t.Error("Missing user_id")
				}
				if req.Content == "" {
					t.Error("Missing content")
				}
				if len(req.Vector) != 768 {
					t.Errorf("Expected 768-dim vector, got %d", len(req.Vector))
				}

				w.WriteHeader(tt.wantStatusCode)
			}))
			defer server.Close()

			svc := &KagentMemoryService{
				agentName: "test-agent",
				apiURL:    server.URL,
				client:    server.Client(),
				ttlDays:   15,
			}

			vector := make([]float32, tt.vectorDim)
			err := svc.storeMemory(context.Background(), tt.userID, tt.content, vector)

			if (err != nil) != tt.wantErr {
				t.Errorf("storeMemory() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestKagentMemoryService_ExtractSessionContent(t *testing.T) {
	tests := []struct {
		name        string
		events      []*adksession.Event
		wantEmpty   bool
		wantContain string
	}{
		{
			name:      "no_events",
			events:    nil,
			wantEmpty: true,
		},
		{
			name: "events_with_text",
			events: []*adksession.Event{
				newMockEvent("user", "Hello"),
				newMockEvent("agent", "Hi there!"),
			},
			wantEmpty:   false,
			wantContain: "user: Hello",
		},
		{
			name: "events_without_text",
			events: []*adksession.Event{
				newMockEventWithFunctionCall("agent", "get_weather"),
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &KagentMemoryService{
				agentName: "test-agent",
			}

			session := newMockSession("sess1", "user1", tt.events)
			content := svc.extractSessionContent(session)

			if tt.wantEmpty && content != "" {
				t.Errorf("Expected empty content, got: %s", content)
			}

			if !tt.wantEmpty && content == "" {
				t.Error("Expected non-empty content, got empty")
			}

			if tt.wantContain != "" && !contains(content, tt.wantContain) {
				t.Errorf("Expected content to contain %q, got: %s", tt.wantContain, content)
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid_config",
			config: Config{
				AgentName: "test-agent",
				APIURL:    "http://localhost:8083",
				EmbeddingConfig: &adk.EmbeddingConfig{
					Provider: "openai",
					Model:    "text-embedding-3-small",
				},
			},
			wantErr: false,
		},
		{
			name: "missing_agent_name",
			config: Config{
				APIURL: "http://localhost:8083",
			},
			wantErr: true,
		},
		{
			name: "missing_api_url",
			config: Config{
				AgentName: "test-agent",
			},
			wantErr: true,
		},
		{
			name: "missing_embedding_config",
			config: Config{
				AgentName: "test-agent",
				APIURL:    "http://localhost:8083",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := New(tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && svc == nil {
				t.Error("Expected non-nil service")
			}

			if !tt.wantErr {
				if svc.agentName != tt.config.AgentName {
					t.Errorf("Expected agent name %s, got %s", tt.config.AgentName, svc.agentName)
				}
			}
		})
	}
}

// Mock implementations

type mockSession struct {
	id      string
	userID  string
	appName string
	events  *mockEvents
}

func newMockSession(id, userID string, events []*adksession.Event) *mockSession {
	return &mockSession{
		id:      id,
		userID:  userID,
		appName: "test-app",
		events:  &mockEvents{events: events},
	}
}

func (m *mockSession) ID() string                { return m.id }
func (m *mockSession) UserID() string            { return m.userID }
func (m *mockSession) AppName() string           { return m.appName }
func (m *mockSession) State() adksession.State   { return nil }
func (m *mockSession) Events() adksession.Events { return m.events }
func (m *mockSession) LastUpdateTime() time.Time { return time.Now() }

type mockEvents struct {
	events []*adksession.Event
}

func (m *mockEvents) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		for _, e := range m.events {
			if !yield(e) {
				return
			}
		}
	}
}

func (m *mockEvents) Len() int {
	return len(m.events)
}

func (m *mockEvents) At(i int) *adksession.Event {
	if i < 0 || i >= len(m.events) {
		return nil
	}
	return m.events[i]
}

func newMockEvent(author, text string) *adksession.Event {
	evt := &adksession.Event{
		ID:           "evt-" + author,
		Author:       author,
		Timestamp:    time.Now(),
		InvocationID: "inv-1",
		Actions: adksession.EventActions{
			StateDelta: make(map[string]any),
		},
	}
	evt.Content = &genai.Content{
		Role: author,
		Parts: []*genai.Part{
			{Text: text},
		},
	}
	return evt
}

func newMockEventWithFunctionCall(author, functionName string) *adksession.Event {
	evt := &adksession.Event{
		ID:           "evt-" + author,
		Author:       author,
		Timestamp:    time.Now(),
		InvocationID: "inv-1",
		Actions: adksession.EventActions{
			StateDelta: make(map[string]any),
		},
	}
	evt.Content = &genai.Content{
		Role: author,
		Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{
				Name: functionName,
			}},
		},
	}
	return evt
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
