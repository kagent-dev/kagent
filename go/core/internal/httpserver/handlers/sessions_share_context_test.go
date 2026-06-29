package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func makeReqWithUser(userID string) *http.Request {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := auth.AuthSessionTo(req.Context(), &authimpl.SimpleSession{
		P: auth.Principal{User: auth.User{ID: userID}},
	})
	return req.WithContext(ctx)
}

func makeReqWithShareContext(userID, shareOwnerID, shareSessionID string) *http.Request {
	req := makeReqWithUser(userID)
	sc := &auth.ShareContext{
		Token:     "tok",
		SessionID: shareSessionID,
		UserID:    shareOwnerID,
		ReadOnly:  true,
	}
	ctx := auth.ShareContextTo(req.Context(), sc)
	return req.WithContext(ctx)
}

func TestGetEffectiveUserIDForSession(t *testing.T) {
	tests := []struct {
		name      string
		req       *http.Request
		sessionID string
		wantID    string
		wantErr   bool
	}{
		{
			name:      "no share context returns caller user ID",
			req:       makeReqWithUser("caller-id"),
			sessionID: "sess-1",
			wantID:    "caller-id",
		},
		{
			name:      "share context matching session returns owner ID",
			req:       makeReqWithShareContext("visitor-id", "owner-id", "sess-1"),
			sessionID: "sess-1",
			wantID:    "owner-id",
		},
		{
			name:      "share context non-matching session falls back to caller user ID",
			req:       makeReqWithShareContext("visitor-id", "owner-id", "sess-other"),
			sessionID: "sess-1",
			wantID:    "visitor-id",
		},
		{
			name:      "no user and no share context returns error",
			req:       httptest.NewRequest("GET", "/", nil),
			sessionID: "sess-1",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getEffectiveUserIDForSession(tt.req, tt.sessionID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("getEffectiveUserIDForSession() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.wantID {
				t.Errorf("getEffectiveUserIDForSession() = %q, want %q", got, tt.wantID)
			}
		})
	}
}
