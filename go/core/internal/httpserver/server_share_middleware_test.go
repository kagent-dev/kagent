package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// stubShareDB only implements GetSessionShareByToken and RecordShareAccess; all other methods panic on call.
type stubShareDB struct {
	dbpkg.Client
	getShare func(ctx context.Context, token string) (*dbpkg.SessionShare, error)
}

func (s *stubShareDB) GetSessionShareByToken(ctx context.Context, token string) (*dbpkg.SessionShare, error) {
	return s.getShare(ctx, token)
}

func (s *stubShareDB) RecordShareAccess(_ context.Context, _ string, _ int64) error {
	return nil
}

func newMiddlewareServer(getShare func(ctx context.Context, token string) (*dbpkg.SessionShare, error)) *HTTPServer {
	return &HTTPServer{
		config: ServerConfig{
			DbClient: &stubShareDB{getShare: getShare},
		},
	}
}

func withUser(r *http.Request, userID string) *http.Request {
	ctx := auth.AuthSessionTo(r.Context(), &authimpl.SimpleSession{
		P: auth.Principal{User: auth.User{ID: userID}},
	})
	return r.WithContext(ctx)
}

func TestShareTokenMiddleware(t *testing.T) {
	okShare := &dbpkg.SessionShare{
		Token:     "valid-token",
		SessionID: "sess-1",
		UserID:    "owner-id",
		ReadOnly:  true,
	}
	rwShare := &dbpkg.SessionShare{
		Token:     "rw-token",
		SessionID: "sess-1",
		UserID:    "owner-id",
		ReadOnly:  false,
	}

	tests := []struct {
		name         string
		getShare     func(ctx context.Context, token string) (*dbpkg.SessionShare, error)
		buildReq     func() *http.Request
		wantStatus   int
		wantShareCtx bool
		wantReadOnly bool
	}{
		{
			name:     "no token passes through without ShareContext",
			getShare: nil, // never called
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
				return withUser(r, "caller-id")
			},
			wantStatus:   http.StatusOK,
			wantShareCtx: false,
		},
		{
			name:     "token without auth session returns 401",
			getShare: nil, // never called
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
				r.Header.Set("X-Share-Token", "some-token")
				return r // no auth session
			},
			wantStatus:   http.StatusUnauthorized,
			wantShareCtx: false,
		},
		{
			name: "invalid token returns 403",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return nil, dbpkg.ErrNotFound
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
				r.Header.Set("X-Share-Token", "bad-token")
				return withUser(r, "caller-id")
			},
			wantStatus:   http.StatusForbidden,
			wantShareCtx: false,
		},
		{
			// Revocation deletes the session_share row; subsequent lookups return database.ErrNotFound,
			// so revoked tokens are rejected immediately — no grace period.
			name: "revoked token returns 403",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return nil, dbpkg.ErrNotFound
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
				r.Header.Set("X-Share-Token", "revoked-token")
				return withUser(r, "visitor-id")
			},
			wantStatus:   http.StatusForbidden,
			wantShareCtx: false,
		},
		{
			name: "valid read-only token with GET passes through with ShareContext",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return okShare, nil
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-1", nil)
				r.Header.Set("X-Share-Token", "valid-token")
				return withUser(r, "visitor-id")
			},
			wantStatus:   http.StatusOK,
			wantShareCtx: true,
			wantReadOnly: true,
		},
		{
			name: "valid read-only token with POST to session path returns 403",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return okShare, nil
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-1/events", nil)
				r.Header.Set("X-Share-Token", "valid-token")
				return withUser(r, "visitor-id")
			},
			wantStatus:   http.StatusForbidden,
			wantShareCtx: false,
		},
		{
			name: "valid read-only token with POST to unrelated path passes through",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return okShare, nil
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/feedback", nil)
				r.Header.Set("X-Share-Token", "valid-token")
				return withUser(r, "visitor-id")
			},
			wantStatus:   http.StatusOK,
			wantShareCtx: true,
			wantReadOnly: true,
		},
		{
			name: "valid read-write token with POST passes through with ShareContext",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return rwShare, nil
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-1/events", nil)
				r.Header.Set("X-Share-Token", "rw-token")
				return withUser(r, "visitor-id")
			},
			wantStatus:   http.StatusOK,
			wantShareCtx: true,
			wantReadOnly: false,
		},
		{
			// A2A is JSON-RPC over POST, so read-only enforcement can't be done by
			// verb here; the middleware passes the request through with a ShareContext
			// and the A2A handler rejects mutating methods per-method.
			name: "valid read-only token with POST to A2A path passes through with ShareContext",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return okShare, nil
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, APIPathA2A+"/default/my-agent", nil)
				r.Header.Set("X-Share-Token", "valid-token")
				return withUser(r, "visitor-id")
			},
			wantStatus:   http.StatusOK,
			wantShareCtx: true,
			wantReadOnly: true,
		},
		{
			name: "valid read-write token with POST to A2A path passes through",
			getShare: func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
				return rwShare, nil
			},
			buildReq: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, APIPathA2A+"/default/my-agent", nil)
				r.Header.Set("X-Share-Token", "rw-token")
				return withUser(r, "visitor-id")
			},
			wantStatus:   http.StatusOK,
			wantShareCtx: true,
			wantReadOnly: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getShare := tt.getShare
			if getShare == nil {
				getShare = func(_ context.Context, _ string) (*dbpkg.SessionShare, error) {
					t.Fatal("GetSessionShareByToken should not have been called")
					return nil, nil
				}
			}

			srv := newMiddlewareServer(getShare)

			var capturedCtx context.Context
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			})

			w := httptest.NewRecorder()
			srv.shareTokenMiddleware(inner).ServeHTTP(w, tt.buildReq())

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if !tt.wantShareCtx {
				if capturedCtx != nil {
					if sc, ok := auth.ShareContextFrom(capturedCtx); ok {
						t.Errorf("expected no ShareContext in context, got %+v", sc)
					}
				}
				return
			}

			if capturedCtx == nil {
				t.Fatal("inner handler was not called")
			}
			sc, ok := auth.ShareContextFrom(capturedCtx)
			if !ok {
				t.Fatal("expected ShareContext in context, got none")
			}
			if sc.ReadOnly != tt.wantReadOnly {
				t.Errorf("ReadOnly = %v, want %v", sc.ReadOnly, tt.wantReadOnly)
			}
			if sc.UserID != "owner-id" {
				t.Errorf("UserID = %q, want %q", sc.UserID, "owner-id")
			}
		})
	}
}
