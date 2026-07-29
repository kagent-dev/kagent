package sts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
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
	plugin := NewTokenPropagationPlugin(nil, logr.Discard())
	bearer := signedTokenWithSub(t, "alice")
	plugin.setCachedToken("sess-123", subjectKey(bearer), "token-abc", 0)

	ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
	headers := plugin.HeaderProvider(fakeSessionContext{
		Context:   ctx,
		sessionID: "sess-123",
	})

	if headers["Authorization"] != "Bearer token-abc" {
		t.Fatalf("Authorization header = %q, want %q", headers["Authorization"], "Bearer token-abc")
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

	plugin := NewTokenPropagationPlugin(integration, logr.Discard())
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

func signedTokenWith(t *testing.T, iss, sub string) string {
	t.Helper()
	claims := jwt.MapClaims{"sub": sub}
	if iss != "" {
		claims["iss"] = iss
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return token
}

func signedTokenWithSub(t *testing.T, sub string) string {
	t.Helper()
	return signedTokenWith(t, "https://issuer.example", sub)
}

// "sub" is only unique within an issuer, so two principals that share a "sub"
// value across different issuers must not collapse onto one cache key.
func TestSubjectKeyDistinguishesIssuers(t *testing.T) {
	t.Parallel()
	if subjectKey(signedTokenWith(t, "iss-a", "same")) == subjectKey(signedTokenWith(t, "iss-b", "same")) {
		t.Fatal("same sub from different issuers must not share a cache key")
	}
}

// A session shared by multiple subjects must run each caller's tool calls under
// that caller's exchanged token, not whichever subject seeded the session first.
func TestSharedSessionKeepsPerSubjectTokens(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         srv.URL,
				"token_endpoint": srv.URL + "/token",
			})
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		// Echo the incoming subject into the issued token so each caller receives
		// a distinct exchanged token.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "exchanged-for-" + subjectKey(r.FormValue("subject_token")),
			"issued_token_type": string(TokenTypeJWT),
		})
	}))
	defer srv.Close()

	integration, err := NewSTSIntegration(
		srv.URL+"/.well-known/oauth-authorization-server",
		"",
		func(context.Context) (string, error) { return "actor", nil },
		nil,
		5,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}

	plugin := NewTokenPropagationPlugin(integration, logr.Discard())

	const sessionID = "shared-session"
	alice := signedTokenWithSub(t, "alice")
	bob := signedTokenWithSub(t, "bob")

	for _, bearer := range []string{alice, bob} {
		ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
		if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: ctx, sessionID: sessionID}); err != nil {
			t.Fatalf("BeforeRunCallback() error = %v", err)
		}
	}

	for _, bearer := range []string{alice, bob} {
		ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
		headers := plugin.HeaderProvider(fakeSessionContext{Context: ctx, sessionID: sessionID})
		want := "Bearer exchanged-for-" + subjectKey(bearer)
		if headers["Authorization"] != want {
			t.Fatalf("Authorization header = %q, want %q", headers["Authorization"], want)
		}
	}
}

// HeaderProvider must recover the acting subject even when the bearer reaches it
// only through the A2A CallContext, the channel the MCP round-tripper reads, and
// not via models.BearerTokenKey. This pins the plumbing the per-subject lookup
// depends on at the transport layer.
func TestHeaderProviderRecoversSubjectFromCallContext(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard())
	const sessionID = "sess-cc"
	alice := signedTokenWithSub(t, "alice")

	// Seed the cache through the executor path (bearer via BearerTokenKey).
	seedCtx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, alice)
	if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: seedCtx, sessionID: sessionID}); err != nil {
		t.Fatalf("BeforeRunCallback() error = %v", err)
	}

	// Look up through the transport path: no BearerTokenKey, bearer only in the
	// A2A CallContext Authorization header.
	ccCtx, _ := a2asrv.WithCallContext(context.Background(),
		a2asrv.NewRequestMeta(map[string][]string{"authorization": {"Bearer " + alice}}))
	headers := plugin.HeaderProvider(fakeSessionContext{Context: ccCtx, sessionID: sessionID})

	if got := headers["Authorization"]; got != "Bearer "+alice {
		t.Fatalf("Authorization header = %q, want %q", got, "Bearer "+alice)
	}
}

// A repeat request from the same subject on the same session reuses the cached
// exchange rather than exchanging again.
func TestBeforeRunCallbackSameSubjectCachesExchange(t *testing.T) {
	t.Parallel()

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
		exchangeCount++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "access",
			"issued_token_type": string(TokenTypeJWT),
		})
	}))
	defer srv.Close()

	integration, err := NewSTSIntegration(
		srv.URL+"/.well-known/oauth-authorization-server",
		"",
		func(context.Context) (string, error) { return "actor", nil },
		nil,
		5,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}

	plugin := NewTokenPropagationPlugin(integration, logr.Discard())
	bearer := signedTokenWithSub(t, "alice")
	for range 2 {
		ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
		if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: ctx, sessionID: "sess"}); err != nil {
			t.Fatalf("BeforeRunCallback() error = %v", err)
		}
	}

	if exchangeCount != 1 {
		t.Fatalf("token exchange calls = %d, want 1", exchangeCount)
	}
}

// A request with no bearer must not receive another subject's cached token.
func TestHeaderProviderNoBearerDoesNotLeakSubjectToken(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard())
	plugin.setCachedToken("sess-x", "alice", "alice-token", 0)

	headers := plugin.HeaderProvider(fakeSessionContext{
		Context:   context.Background(),
		sessionID: "sess-x",
	})

	if got, ok := headers["Authorization"]; ok {
		t.Fatalf("expected no Authorization header for empty-bearer request, got %q", got)
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
