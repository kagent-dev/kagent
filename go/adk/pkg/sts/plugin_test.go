package sts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
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
	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
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

	plugin := NewTokenPropagationPlugin(integration, logr.Discard(), nil, nil)
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

			plugin := NewTokenPropagationPlugin(integration, logr.Discard(), tt.resource, tt.audience)
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

func signedTokenWithKey(t *testing.T, iss, sub, signingKey string) string {
	t.Helper()
	claims := jwt.MapClaims{"sub": sub}
	if iss != "" {
		claims["iss"] = iss
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(signingKey))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return token
}

func signedTokenWithSub(t *testing.T, sub string) string {
	t.Helper()
	return signedTokenWithKey(t, "https://issuer.example", sub, "secret")
}

// A cache hit hands out a delegated token without an STS exchange, so the key
// must not be derivable from claims anyone can write: a token carrying the
// victim's "iss" and "sub" must miss the victim's entry and be sent to the STS,
// which is what rejects it.
func TestSubjectKeyIgnoresUnverifiedClaims(t *testing.T) {
	t.Parallel()
	genuine := signedTokenWithKey(t, "https://issuer.example", "alice", "genuine-signing-key")
	forged := signedTokenWithKey(t, "https://issuer.example", "alice", "attacker-signing-key")
	if subjectKey(genuine) == subjectKey(forged) {
		t.Fatal("a token claiming the victim's iss/sub must not share the victim's cache key")
	}
}

// Distinct credentials must partition, and no credential must yield no key.
func TestSubjectKeyPartitionsOpaqueTokens(t *testing.T) {
	t.Parallel()
	if subjectKey("opaque-a") == subjectKey("opaque-b") {
		t.Fatal("distinct opaque tokens must not share a cache key")
	}
	if got := subjectKey(""); got != "" {
		t.Fatalf("subjectKey(\"\") = %q, want an empty key", got)
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

	plugin := NewTokenPropagationPlugin(integration, logr.Discard(), nil, nil)

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

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	const sessionID = "sess-cc"
	alice := signedTokenWithSub(t, "alice")

	// Seed the cache through the executor path (bearer via BearerTokenKey).
	seedCtx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, alice)
	if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: seedCtx, sessionID: sessionID}); err != nil {
		t.Fatalf("BeforeRunCallback() error = %v", err)
	}

	// Look up through the transport path: no BearerTokenKey, bearer only in the
	// A2A CallContext Authorization header.
	ccCtx, _ := a2asrv.NewCallContext(context.Background(),
		a2asrv.NewServiceParams(map[string][]string{"authorization": {"Bearer " + alice}}))
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

	plugin := NewTokenPropagationPlugin(integration, logr.Discard(), nil, nil)
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

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	plugin.setCachedToken("sess-x", "alice", "alice-token", 0)

	headers := plugin.HeaderProvider(fakeSessionContext{
		Context:   context.Background(),
		sessionID: "sess-x",
	})

	if got, ok := headers["Authorization"]; ok {
		t.Fatalf("expected no Authorization header for empty-bearer request, got %q", got)
	}
}

// An empty subject identifies no principal, so it must not be storable: an entry
// under it would be shared by every credential-less caller in the session.
func TestEmptySubjectIsNotCacheable(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	plugin.setCachedToken("sess-x", "", "anonymous-token", 0)

	if len(plugin.tokenCache) != 0 {
		t.Fatalf("expected empty subject not to be cached, got %d entries", len(plugin.tokenCache))
	}
	if _, ok := plugin.getCachedToken("sess-x", ""); ok {
		t.Fatal("expected no cache hit for an empty subject")
	}
}

// The forged-token case end to end: a caller presenting a token that merely
// claims to be alice must not be handed alice's cached entry.
func TestHeaderProviderRejectsForgedSubjectClaims(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	const sessionID = "sess-forge"
	alice := signedTokenWithKey(t, "https://issuer.example", "alice", "genuine-signing-key")

	seedCtx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, alice)
	if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: seedCtx, sessionID: sessionID}); err != nil {
		t.Fatalf("BeforeRunCallback() error = %v", err)
	}

	forged := signedTokenWithKey(t, "https://issuer.example", "alice", "attacker-signing-key")
	forgedCtx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, forged)
	headers := plugin.HeaderProvider(fakeSessionContext{Context: forgedCtx, sessionID: sessionID})

	if got, ok := headers["Authorization"]; ok {
		t.Fatalf("forged token must not receive a cached entry, got %q", got)
	}
}

// A token with no exp claim must still get a bounded cache lifetime: the cache
// holds one entry per (session, subject), so an immortal entry per caller would
// grow without limit.
func TestCachedTokenWithoutExpiryStaysEvictable(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	plugin.setCachedToken("sess-ttl", "alice", "opaque-token", 0)

	entry, ok := plugin.getCachedToken("sess-ttl", "alice")
	if !ok {
		t.Fatal("expected a cached entry")
	}
	if entry.Expiry == 0 {
		t.Fatal("entry with no exp claim must be given a bounded expiry")
	}
	if ceiling := time.Now().Add(maxCacheTTL).Unix(); entry.Expiry > ceiling {
		t.Fatalf("entry expiry %d exceeds the %s ceiling %d", entry.Expiry, maxCacheTTL, ceiling)
	}
}

// The sweep must evict entries belonging to subjects and sessions other than the
// acting one, since only the acting subject's key is derivable in the callback.
func TestAfterRunCallbackEvictsExpiredEntriesOfOtherSubjects(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	past := time.Now().Add(-time.Hour).Unix()
	future := time.Now().Add(time.Hour).Unix()
	plugin.setCachedToken("sess-a", "alice", "alice-token", past)
	plugin.setCachedToken("sess-b", "bob", "bob-token", future)

	plugin.AfterRunCallback(&fakeInvocationContext{Context: context.Background(), sessionID: "sess-b"})

	if _, ok := plugin.getCachedToken("sess-a", "alice"); ok {
		t.Fatal("expired entry of another session/subject must be evicted")
	}
	if _, ok := plugin.getCachedToken("sess-b", "bob"); !ok {
		t.Fatal("unexpired entry must survive the sweep")
	}
	if plugin.earliestExpiry != future {
		t.Fatalf("earliestExpiry = %d, want %d after the sweep", plugin.earliestExpiry, future)
	}
}

// The earliest expiry gates the walk, so a cache with nothing evictable is left
// untouched instead of being traversed on every run.
func TestAfterRunCallbackSkipsWalkUntilSomethingExpires(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	future := time.Now().Add(time.Hour).Unix()
	plugin.setCachedToken("sess-a", "alice", "alice-token", future)

	plugin.AfterRunCallback(&fakeInvocationContext{Context: context.Background(), sessionID: "sess-a"})

	if _, ok := plugin.getCachedToken("sess-a", "alice"); !ok {
		t.Fatal("unexpired entry must survive")
	}
	if plugin.earliestExpiry != future {
		t.Fatalf("earliestExpiry = %d, want it left at %d", plugin.earliestExpiry, future)
	}
}

func TestClearCacheResetsEarliestExpiry(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, logr.Discard(), nil, nil)
	plugin.setCachedToken("sess-a", "alice", "alice-token", time.Now().Add(time.Hour).Unix())
	plugin.ClearCache()

	if plugin.earliestExpiry != 0 {
		t.Fatalf("earliestExpiry = %d, want 0 after ClearCache", plugin.earliestExpiry)
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
