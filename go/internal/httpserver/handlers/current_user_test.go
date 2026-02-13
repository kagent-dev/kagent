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
		wantUser       string
		wantEmail      string
		wantName       string
		wantGroups     []string
	}{
		{
			name: "returns user info from session",
			session: &mockSession{
				principal: auth.Principal{
					User: auth.User{
						ID:    "user123",
						Email: "user@example.com",
						Name:  "Test User",
					},
					Groups: []string{"admin", "developers"},
				},
			},
			wantStatusCode: http.StatusOK,
			wantUser:       "user123",
			wantEmail:      "user@example.com",
			wantName:       "Test User",
			wantGroups:     []string{"admin", "developers"},
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
				var response handlers.CurrentUserResponse
				if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}

				if response.User != tt.wantUser {
					t.Errorf("User = %q, want %q", response.User, tt.wantUser)
				}
				if response.Email != tt.wantEmail {
					t.Errorf("Email = %q, want %q", response.Email, tt.wantEmail)
				}
				if response.Name != tt.wantName {
					t.Errorf("Name = %q, want %q", response.Name, tt.wantName)
				}
			}
		})
	}
}
