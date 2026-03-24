package taskstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

func TestSave_NormalizesHitlTaskPersistence(t *testing.T) {
	t.Helper()

	var persisted map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&persisted); err != nil {
			t.Fatalf("failed to decode persisted task: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	store := NewKAgentTaskStoreWithClient(server.URL, server.Client())
	task := &a2atype.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		History: []*a2atype.Message{
			a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.DataPart{
				Data: map[string]any{
					"name": "k8s_get_resources",
					"id":   "call-1",
					"args": map[string]any{"namespace": "default"},
				},
				Metadata: map[string]any{
					"adk_type":            "function_call",
					"adk_is_long_running": false,
				},
			}),
			a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.DataPart{
				Data: map[string]any{
					"name": "k8s_get_resources",
					"id":   "call-1",
					"response": map[string]any{
						"status": "confirmation_requested",
						"tool":   "k8s_get_resources",
					},
				},
				Metadata: map[string]any{
					"adk_type": "function_response",
				},
			}),
			a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.DataPart{
				Data: map[string]any{
					"name": "adk_request_confirmation",
					"id":   "confirm-1",
					"args": map[string]any{
						"originalFunctionCall": map[string]any{
							"name": "k8s_get_resources",
							"id":   "call-1",
							"args": map[string]any{"namespace": "default"},
						},
						"toolConfirmation": map[string]any{
							"confirmed": false,
						},
					},
				},
				Metadata: map[string]any{
					"adk_type":            "function_call",
					"adk_is_long_running": true,
				},
			}),
			a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.DataPart{
				Data: map[string]any{
					"name": "k8s_get_resources",
					"id":   "call-1",
					"response": map[string]any{
						"output": "NAME   DATA\ncfg    1\n",
					},
				},
				Metadata: map[string]any{
					"adk_type": "function_response",
				},
			}),
		},
		Status: a2atype.TaskStatus{
			State: a2atype.TaskStateInputRequired,
			Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.DataPart{
				Data: map[string]any{
					"name": "k8s_get_resources",
					"id":   "call-1",
					"response": map[string]any{
						"status": "confirmation_requested",
						"tool":   "k8s_get_resources",
					},
				},
				Metadata: map[string]any{
					"adk_type": "function_response",
				},
			}),
		},
	}

	if err := store.Save(context.Background(), task); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	history, _ := persisted["history"].([]any)
	if len(history) != 3 {
		t.Fatalf("persisted history length = %d, want 3", len(history))
	}

	secondMsg, _ := history[1].(map[string]any)
	secondParts, _ := secondMsg["parts"].([]any)
	secondPart, _ := secondParts[0].(map[string]any)
	secondData, _ := secondPart["data"].(map[string]any)
	if got := secondData["name"]; got != "adk_request_confirmation" {
		t.Fatalf("history[1] name = %v, want adk_request_confirmation", got)
	}

	thirdMsg, _ := history[2].(map[string]any)
	thirdParts, _ := thirdMsg["parts"].([]any)
	thirdPart, _ := thirdParts[0].(map[string]any)
	thirdData, _ := thirdPart["data"].(map[string]any)
	thirdResponse, _ := thirdData["response"].(map[string]any)
	if got, _ := thirdResponse["output"].(string); got != "NAME   DATA\ncfg    1\n" {
		t.Fatalf("tool response output = %v, want original text", got)
	}

	status, _ := persisted["status"].(map[string]any)
	statusMessage, _ := status["message"].(map[string]any)
	statusParts, _ := statusMessage["parts"].([]any)
	statusPart, _ := statusParts[0].(map[string]any)
	statusData, _ := statusPart["data"].(map[string]any)
	if got := statusData["name"]; got != "adk_request_confirmation" {
		t.Fatalf("status.message name = %v, want adk_request_confirmation", got)
	}
}
