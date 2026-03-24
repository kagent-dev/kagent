package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return b
}

func newService(t *testing.T, mux *http.ServeMux) *KAgentSessionService {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewKAgentSessionService(srv.URL, srv.Client())
}

func TestCreate_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(mustJSON(t, map[string]any{"data": map[string]any{"id": "sess-1", "user_id": "user-1"}}))
	})

	svc := newService(t, mux)
	resp, err := svc.Create(context.Background(), &adksession.CreateRequest{
		AppName:   "app",
		UserID:    "user-1",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if resp.Session.ID() != "sess-1" {
		t.Errorf("session ID = %q, want sess-1", resp.Session.ID())
	}
	if resp.Session.UserID() != "user-1" {
		t.Errorf("user ID = %q, want user-1", resp.Session.UserID())
	}
	if resp.Session.AppName() != "app" {
		t.Errorf("app name = %q, want app", resp.Session.AppName())
	}
}

func TestCreate_SessionNameInRequest(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, map[string]any{"data": map[string]any{"id": "s", "user_id": "u"}}))
	})

	svc := newService(t, mux)
	svc.Create(context.Background(), &adksession.CreateRequest{
		AppName: "app",
		UserID:  "u",
		State:   map[string]any{"session_name": "My Session"},
	})

	if gotBody["name"] != "My Session" {
		t.Errorf("name in request body = %v, want 'My Session'", gotBody["name"])
	}
}

func TestGet_DeserializesEvents(t *testing.T) {
	event := map[string]any{
		"invocation_id": "inv-1",
		"author":        "agent",
	}
	eventJSON, _ := json.Marshal(event)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"data": map[string]any{
				"session": map[string]any{"id": "sess-1", "user_id": "u"},
				"events":  []any{map[string]any{"data": json.RawMessage(eventJSON)}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, body))
	})

	svc := newService(t, mux)
	resp, err := svc.Get(context.Background(), &adksession.GetRequest{
		AppName:   "app",
		UserID:    "u",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	evts := EventsFromSession(resp.Session)
	if len(evts) != 1 {
		t.Fatalf("events count = %d, want 1", len(evts))
	}
	if evts[0].Author != "agent" {
		t.Errorf("event author = %q, want agent", evts[0].Author)
	}
}

func TestGet_EmptyEventsSkipped(t *testing.T) {
	// Events with no meaningful content should be silently dropped.
	emptyEvent := map[string]any{} // no author, invocation_id, content, etc.
	emptyJSON, _ := json.Marshal(emptyEvent)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/sess-3", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"data": map[string]any{
				"session": map[string]any{"id": "sess-3", "user_id": "u"},
				"events":  []any{map[string]any{"data": json.RawMessage(emptyJSON)}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, body))
	})

	svc := newService(t, mux)
	resp, err := svc.Get(context.Background(), &adksession.GetRequest{
		AppName: "app", UserID: "u", SessionID: "sess-3",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if n := len(EventsFromSession(resp.Session)); n != 0 {
		t.Errorf("empty event should be skipped, got %d events", n)
	}
}

func TestGet_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/missing", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	svc := newService(t, mux)
	_, err := svc.Get(context.Background(), &adksession.GetRequest{
		AppName: "app", UserID: "u", SessionID: "missing",
	})
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

func TestGetSession_NotFoundReturnsNilSession(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/missing", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	svc := newService(t, mux)
	sess, err := svc.GetSession(context.Background(), "app", "u", "missing")
	if err != nil {
		t.Fatalf("GetSession() error = %v, want nil for not-found", err)
	}
	if sess != nil {
		t.Fatalf("GetSession() session = %#v, want nil", sess)
	}
}

func TestGetSession_BackendErrorIsReturned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/broken", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	})

	svc := newService(t, mux)
	sess, err := svc.GetSession(context.Background(), "app", "u", "broken")
	if err == nil {
		t.Fatal("GetSession() error = nil, want backend error")
	}
	if sess != nil {
		t.Fatalf("GetSession() session = %#v, want nil on backend error", sess)
	}
}

func TestDelete_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	svc := newService(t, mux)
	err := svc.Delete(context.Background(), &adksession.DeleteRequest{
		AppName:   "app",
		UserID:    "u",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestAppendEvent_PersistsAndUpdatesLocalSession(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/sess-1/events", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	})

	svc := newService(t, mux)
	ls := &localSession{
		appName:   "app",
		userID:    "u",
		sessionID: "sess-1",
		state:     make(map[string]any),
	}

	event := &adksession.Event{ID: "evt-1", Author: "agent"}
	if err := svc.AppendEvent(context.Background(), ls, event); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	// Remote call received the event ID.
	if gotBody["id"] != "evt-1" {
		t.Errorf("persisted event ID = %v, want evt-1", gotBody["id"])
	}
	// Local session is updated.
	evts := EventsFromSession(ls)
	if len(evts) != 1 {
		t.Fatalf("local events count = %d, want 1", len(evts))
	}
}

func TestCreateSession_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustJSON(t, map[string]any{"data": map[string]any{"id": "s", "user_id": "u"}}))
	})

	svc := newService(t, mux)
	err := svc.CreateSession(context.Background(), "app", "u", nil, "s")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
}

func TestEventsFromSession_LocalSession(t *testing.T) {
	e1 := &adksession.Event{ID: "e1", Author: "agent"}
	e2 := &adksession.Event{ID: "e2", Author: "user"}
	ls := &localSession{
		sessionID: "s",
		events:    []*adksession.Event{e1, e2},
		state:     make(map[string]any),
	}

	got := EventsFromSession(ls)
	if len(got) != 2 {
		t.Fatalf("EventsFromSession() len = %d, want 2", len(got))
	}
	if got[0].ID != "e1" || got[1].ID != "e2" {
		t.Errorf("EventsFromSession() = %v", got)
	}
}

func TestNormalizeConfirmationEventRole(t *testing.T) {
	tests := []struct {
		name     string
		event    *adksession.Event
		wantRole string
	}{
		{
			name:  "nil event is safe",
			event: nil,
		},
		{
			name: "nil content is safe",
			event: &adksession.Event{
				Author: "agent",
			},
		},
		{
			name: "adk_request_confirmation with role model is fixed to user",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Role: string(genai.RoleModel),
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: "adk_request_confirmation",
									ID:   "adk-1",
								},
							},
						},
					},
				},
			},
			wantRole: string(genai.RoleUser),
		},
		{
			name: "batch adk_request_confirmation with role model is fixed to user",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Role: string(genai.RoleModel),
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: "adk_request_confirmation",
									ID:   "adk-1",
								},
							},
							{
								FunctionCall: &genai.FunctionCall{
									Name: "adk_request_confirmation",
									ID:   "adk-2",
								},
							},
						},
					},
				},
			},
			wantRole: string(genai.RoleUser),
		},
		{
			name: "already role user is not changed",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Role: string(genai.RoleUser),
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: "adk_request_confirmation",
									ID:   "adk-1",
								},
							},
						},
					},
				},
			},
			wantRole: string(genai.RoleUser),
		},
		{
			name: "model event with regular tool call is NOT changed",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Role: string(genai.RoleModel),
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: "k8s_get_resources",
									ID:   "call-1",
								},
							},
						},
					},
				},
			},
			wantRole: string(genai.RoleModel),
		},
		{
			name: "mixed parts with non-confirmation call is NOT changed",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Role: string(genai.RoleModel),
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: "adk_request_confirmation",
									ID:   "adk-1",
								},
							},
							{
								FunctionCall: &genai.FunctionCall{
									Name: "some_other_tool",
									ID:   "call-2",
								},
							},
						},
					},
				},
			},
			wantRole: string(genai.RoleModel),
		},
		{
			name: "text event is NOT changed",
			event: &adksession.Event{
				LLMResponse: adkmodel.LLMResponse{
					Content: &genai.Content{
						Role:  string(genai.RoleModel),
						Parts: []*genai.Part{{Text: "hello"}},
					},
				},
			},
			wantRole: string(genai.RoleModel),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizeConfirmationEventRole(tt.event)
			if tt.event != nil && tt.event.Content != nil {
				if tt.event.Content.Role != tt.wantRole {
					t.Errorf("role = %q, want %q", tt.event.Content.Role, tt.wantRole)
				}
			}
		})
	}
}
