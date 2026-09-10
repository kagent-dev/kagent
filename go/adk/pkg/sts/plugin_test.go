package sts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"

	"github.com/golang-jwt/jwt/v5"
	kagentmodels "github.com/kagent-dev/kagent/go/adk/pkg/models"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type fakeSessionContext struct {
	context.Context
	sessionID string
}

func (f fakeSessionContext) SessionID() string {
	return f.sessionID
}

type fakeInvocationContext struct {
	context.Context
	sessionID string
	ended     bool
}

func (f fakeInvocationContext) Agent() agent.Agent              { return nil }
func (f fakeInvocationContext) Artifacts() agent.Artifacts      { return nil }
func (f fakeInvocationContext) Memory() agent.Memory            { return nil }
func (f fakeInvocationContext) Session() session.Session        { return fakeSession{id: f.sessionID} }
func (f fakeInvocationContext) InvocationID() string            { return "" }
func (f fakeInvocationContext) Branch() string                  { return "" }
func (f fakeInvocationContext) IsolationScope() string          { return "" }
func (f fakeInvocationContext) UserContent() *genai.Content     { return nil }
func (f fakeInvocationContext) RunConfig() *agent.RunConfig     { return nil }
func (f *fakeInvocationContext) EndInvocation()                 { f.ended = true }
func (f fakeInvocationContext) Ended() bool                     { return f.ended }
func (f fakeInvocationContext) ResumedInput(string) (any, bool) { return nil, false }
func (f fakeInvocationContext) WithContext(ctx context.Context) agent.InvocationContext {
	f.Context = ctx
	return &f
}
func (f fakeInvocationContext) WithICDelta(*agent.InvocationContextDelta) agent.InvocationContext {
	return &f
}

type fakeSession struct {
	id string
}

func (f fakeSession) ID() string                { return f.id }
func (f fakeSession) AppName() string           { return "" }
func (f fakeSession) UserID() string            { return "" }
func (f fakeSession) State() session.State      { return nil }
func (f fakeSession) Events() session.Events    { return nil }
func (f fakeSession) LastUpdateTime() time.Time { return time.Time{} }

func TestHeaderProvider_UsesSessionIDMethod(t *testing.T) {
	t.Parallel()
	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	plugin.setCachedToken("sess-123", "token-abc", 0)

	headers := plugin.HeaderProvider(fakeSessionContext{
		Context:   context.Background(),
		sessionID: "sess-123",
	})

	if headers["Authorization"] != "Bearer token-abc" {
		t.Fatalf("Authorization header = %q, want %q", headers["Authorization"], "Bearer token-abc")
	}
}

// TestHeaderProvider_RecoversSessionID pins the identity HeaderProvider presents
// for each shape of context it can be handed on the outbound MCP path. The
// deadline-wrapped case is the regression: a type assertion stops matching there.
func TestHeaderProvider_RecoversSessionID(t *testing.T) {
	t.Parallel()

	const (
		sessionID      = "01a01e53-cfc7-7c25-9783-d0e5203b6451"
		exchangedToken = "EXCHANGED-STS-TOKEN"
	)

	// valueCtx is the context the A2A executor produces: the session ID stored
	// as a value, not exposed as a method.
	valueCtx := func() context.Context {
		return context.WithValue(context.Background(), kagentmodels.SessionIDKey, sessionID)
	}

	tests := []struct {
		name  string
		ctx   func(t *testing.T) context.Context
		cache bool // seed the exchanged token for sessionID
		want  string
	}{
		{
			name:  "session as context value",
			ctx:   func(*testing.T) context.Context { return valueCtx() },
			cache: true,
			want:  "Bearer " + exchangedToken,
		},
		{
			name: "session as context value, wrapped in a deadline context",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(valueCtx(), time.Minute)
				t.Cleanup(cancel)
				return ctx
			},
			cache: true,
			want:  "Bearer " + exchangedToken,
		},
		{
			// A caller still holding ADK's ToolContext keeps working.
			name: "session only via SessionID()",
			ctx: func(*testing.T) context.Context {
				return fakeSessionContext{Context: context.Background(), sessionID: sessionID}
			},
			cache: true,
			want:  "Bearer " + exchangedToken,
		},
		{
			// Startup toolset discovery: a plain context, no user to act for.
			name: "no session",
			ctx:  func(*testing.T) context.Context { return context.Background() },
			want: "",
		},
		{
			// A user is present but their exchange produced nothing: no header,
			// so the upstream rejects rather than seeing another identity.
			name: "session present but no cached token",
			ctx:  func(*testing.T) context.Context { return valueCtx() },
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
			if tt.cache {
				plugin.setCachedToken(sessionID, exchangedToken, 0)
			}

			if got := plugin.HeaderProvider(tt.ctx(t))["Authorization"]; got != tt.want {
				t.Fatalf("Authorization header = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHeaderProvider_ContextValueBeatsSessionIDMethod: when both mechanisms
// disagree, the value the executor stamped for this request wins.
func TestHeaderProvider_ContextValueBeatsSessionIDMethod(t *testing.T) {
	t.Parallel()

	const (
		valueSession  = "session-from-value"
		methodSession = "session-from-method"
	)

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	plugin.setCachedToken(valueSession, "TOKEN-FOR-VALUE", 0)
	plugin.setCachedToken(methodSession, "TOKEN-FOR-METHOD", 0)

	headers := plugin.HeaderProvider(fakeSessionContext{
		Context:   context.WithValue(context.Background(), kagentmodels.SessionIDKey, valueSession),
		sessionID: methodSession,
	})

	if got := headers["Authorization"]; got != "Bearer TOKEN-FOR-VALUE" {
		t.Fatalf("Authorization header = %q, want %q", got, "Bearer TOKEN-FOR-VALUE")
	}
}

func TestBeforeRunCallback_ReusesCachedDynamicActorTokenForExchange(t *testing.T) {
	t.Parallel()

	fetchCount := 0
	exchangeCount := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         srv.URL,
				"token_endpoint": srv.URL + "/token",
			})
			return
		}
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		exchangeCount++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.FormValue("actor_token"); got != "dynamic-actor" {
			t.Fatalf("actor_token = %q, want %q", got, "dynamic-actor")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "access-token",
			"issued_token_type": string(TokenTypeJWT),
		})
	}))
	defer srv.Close()

	integration, err := NewSTSIntegration(
		srv.URL+"/.well-known/oauth-authorization-server",
		"",
		func(context.Context) (string, error) {
			fetchCount++
			return "dynamic-actor", nil
		},
		nil,
		5,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}

	plugin := NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)
	for _, sessionID := range []string{"sess-one", "sess-two"} {
		ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, "subject-token")
		if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{
			Context:   ctx,
			sessionID: sessionID,
		}); err != nil {
			t.Fatalf("BeforeRunCallback() error = %v", err)
		}
	}

	if fetchCount != 1 {
		t.Fatalf("fetchActorToken calls = %d, want 1", fetchCount)
	}
	if exchangeCount != 2 {
		t.Fatalf("token exchange calls = %d, want 2", exchangeCount)
	}
}

func TestBeforeRunCallback_SendsResourceAndAudience(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resource     []string
		audience     []string
		wantResource string
		wantAudience string
	}{
		{
			name:         "configured target is sent",
			resource:     []string{"https://mcp.example.com"},
			audience:     []string{"mcp-backend"},
			wantResource: "https://mcp.example.com",
			wantAudience: "mcp-backend",
		},
		{
			name:         "no target leaves resource and audience unset",
			wantResource: "",
			wantAudience: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			type exchangeForm struct {
				resource string
				audience string
				err      error
			}
			// Buffered so the handler never blocks on send; the value is read
			// back on the test goroutine to avoid a data race on the captured form.
			gotForm := make(chan exchangeForm, 1)

			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/oauth-authorization-server" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"issuer":         srv.URL,
						"token_endpoint": srv.URL + "/token",
					})
					return
				}
				if r.URL.Path != "/token" {
					http.NotFound(w, r)
					return
				}
				if err := r.ParseForm(); err != nil {
					gotForm <- exchangeForm{err: err}
				} else {
					gotForm <- exchangeForm{resource: r.FormValue("resource"), audience: r.FormValue("audience")}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":      "access-token",
					"issued_token_type": string(TokenTypeJWT),
				})
			}))
			defer srv.Close()

			integration, err := NewSTSIntegration(
				srv.URL+"/.well-known/oauth-authorization-server",
				"", nil, nil, 5, true, false,
			)
			if err != nil {
				t.Fatalf("NewSTSIntegration() error = %v", err)
			}

			plugin := NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), tt.resource, tt.audience)
			ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, "subject-token")
			if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{
				Context:   ctx,
				sessionID: "sess-resource",
			}); err != nil {
				t.Fatalf("BeforeRunCallback() error = %v", err)
			}

			select {
			case got := <-gotForm:
				if got.err != nil {
					t.Fatalf("ParseForm() error = %v", got.err)
				}
				if got.resource != tt.wantResource {
					t.Fatalf("resource = %q, want %q", got.resource, tt.wantResource)
				}
				if got.audience != tt.wantAudience {
					t.Fatalf("audience = %q, want %q", got.audience, tt.wantAudience)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for token exchange request")
			}
		})
	}
}

func TestExtractJWTExpiryUsesUnverifiedClaims(t *testing.T) {
	t.Parallel()
	want := time.Now().Add(time.Hour).Unix()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": want,
	}).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	if got := extractJWTExpiry(token); got != want {
		t.Fatalf("extractJWTExpiry() = %d, want %d", got, want)
	}
}
