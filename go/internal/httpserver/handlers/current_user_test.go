package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

type mockSession struct {
	principal auth.Principal
}

func (m *mockSession) Principal() auth.Principal {
	return m.principal
}

func TestHandleGetCurrentUser(t *testing.T) {
	tests := []struct {
		name           string
		session        auth.Session
		wantStatusCode int
		wantResponse   map[string]any
	}{
		{
			name: "returns raw claims from JWT session",
			session: &mockSession{
				principal: auth.Principal{
					User: auth.User{ID: "user123"},
					Claims: map[string]any{
						"sub":    "user123",
						"email":  "user@example.com",
						"name":   "Test User",
						"groups": []any{"admin", "developers"},
					},
				},
			},
			wantStatusCode: http.StatusOK,
			wantResponse: map[string]any{
				"sub":   "user123",
				"email": "user@example.com",
				"name":  "Test User",
			},
		},
		{
			name: "returns sub-only map for non-JWT session",
			session: &mockSession{
				principal: auth.Principal{
					User: auth.User{ID: "admin@kagent.dev"},
				},
			},
			wantStatusCode: http.StatusOK,
			wantResponse: map[string]any{
				"sub": "admin@kagent.dev",
			},
		},
		{
			name:           "returns 401 when no session",
			session:        nil,
			wantStatusCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.NewCurrentUserHandler()

			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			if tt.session != nil {
				ctx := auth.AuthSessionTo(req.Context(), tt.session)
				req = req.WithContext(ctx)
			}

			rr := httptest.NewRecorder()
			handler.HandleGetCurrentUser(rr, req)

			if rr.Code != tt.wantStatusCode {
				t.Errorf("status code = %d, want %d", rr.Code, tt.wantStatusCode)
			}

			if tt.wantStatusCode == http.StatusOK {
				var response map[string]any
				if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}

				for k, wantV := range tt.wantResponse {
					gotV, ok := response[k]
					if !ok {
						t.Errorf("response missing key %q", k)
						continue
					}
					if wantStr, ok := wantV.(string); ok {
						if gotStr, ok := gotV.(string); !ok || gotStr != wantStr {
							t.Errorf("response[%q] = %v, want %q", k, gotV, wantStr)
						}
					}
				}
			}
		})
	}
}
