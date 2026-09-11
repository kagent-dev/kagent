package sts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
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

// newSTSIntegration wires an STSIntegration to a fake authorization server whose
// token endpoint answers with issue(r); every other path is discovery. issue
// runs on the server goroutine, so it must not call t.Fatal.
func newSTSIntegration(t *testing.T, fetchActor func(context.Context) (string, error), issue func(*http.Request) map[string]any) *STSIntegration {
	t.Helper()

	return newSTSIntegrationWithTokenHandler(t, fetchActor, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(issue(r))
	})
}

// newSTSIntegrationWithTokenHandler serves the token endpoint with handler, so a
// test can answer an exchange with a failure status instead of a token.
func newSTSIntegrationWithTokenHandler(t *testing.T, fetchActor func(context.Context) (string, error), handler http.HandlerFunc) *STSIntegration {
	t.Helper()

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
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	integration, err := NewSTSIntegration(
		srv.URL+"/.well-known/oauth-authorization-server",
		"",
		fetchActor,
		nil,
		5,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}
	return integration
}

// staticActor is an actor-token provider that always returns the same token.
func staticActor(token string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return token, nil }
}

// issued is a token endpoint response carrying accessToken.
func issued(accessToken string) map[string]any {
	return map[string]any{
		"access_token":      accessToken,
		"issued_token_type": string(TokenTypeJWT),
	}
}

func TestBeforeRunCallback_ReusesCachedDynamicActorTokenForExchange(t *testing.T) {
	t.Parallel()

	fetchCount := 0
	exchangeCount := 0
	gotActorToken := ""
	integration := newSTSIntegration(t,
		func(context.Context) (string, error) {
			fetchCount++
			return "dynamic-actor", nil
		},
		func(r *http.Request) map[string]any {
			exchangeCount++
			gotActorToken = r.FormValue("actor_token")
			return issued("access-token")
		},
	)

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
	if gotActorToken != "dynamic-actor" {
		t.Fatalf("actor_token = %q, want %q", gotActorToken, "dynamic-actor")
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

			integration := newSTSIntegration(t, nil, func(r *http.Request) map[string]any {
				gotForm <- exchangeForm{resource: r.FormValue("resource"), audience: r.FormValue("audience")}
				return issued("access-token")
			})

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

	// Echo the incoming subject into the issued token so each caller receives a
	// distinct exchanged token.
	integration := newSTSIntegration(t, staticActor("actor"), func(r *http.Request) map[string]any {
		return issued("exchanged-for-" + subjectKey(r.FormValue("subject_token")))
	})

	plugin := NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)

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

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
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
	integration := newSTSIntegration(t, staticActor("actor"), func(*http.Request) map[string]any {
		exchangeCount++
		return issued("access")
	})

	plugin := NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)
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

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
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

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
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

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
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
// grow without limit. The bound is the eviction time, never the expiry, which
// would end the caller's authority on a deadline nothing stated.
func TestCachedTokenWithoutExpiryStaysEvictable(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	plugin.setCachedToken("sess-ttl", "alice", "opaque-token", 0)

	entry, ok := plugin.getCachedToken("sess-ttl", "alice")
	if !ok {
		t.Fatal("expected a cached entry")
	}
	if entry.Expiry != 0 {
		t.Fatalf("entry expiry = %d, want 0: nothing on this path states one", entry.Expiry)
	}
	if entry.EvictAfter == 0 {
		t.Fatal("entry with no exp claim must be given a bounded eviction time")
	}
	if ceiling := time.Now().Add(maxCacheTTL).Unix(); entry.EvictAfter > ceiling {
		t.Fatalf("entry eviction time %d exceeds the %s ceiling %d", entry.EvictAfter, maxCacheTTL, ceiling)
	}
}

// A run that is still making tool calls keeps its entry when a concurrent run
// finishes and sweeps. BeforeRunCallback runs once per run, so a swept entry has
// nothing to re-mint it and the rest of that run would reach the backend with no
// credential at all.
func TestSweepKeepsAnInFlightCallerStillUsingItsEntry(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	const sessionID = "sess-in-flight"
	const bearer = "opaque-caller-bearer"

	// An opaque credential states no expiry, so the entry carries only the
	// synthetic eviction bound.
	plugin.setCachedToken(sessionID, subjectKey(bearer), "delegated-opaque", 0)

	// The bound comes due while the run is still in flight.
	due := time.Now().Add(-time.Minute).Unix()
	plugin.tokenCache[cacheKey{sessionID: sessionID, subject: subjectKey(bearer)}].EvictAfter = due
	plugin.earliestEviction = due

	// The run makes another tool call, which renews the bound.
	ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
	toolCall := fakeSessionContext{Context: ctx, sessionID: sessionID}
	if got := plugin.HeaderProvider(toolCall)["Authorization"]; got != "Bearer delegated-opaque" {
		t.Fatalf("in-flight tool call got Authorization %q", got)
	}

	// A concurrent run finishes and sweeps the whole cache.
	plugin.AfterRunCallback(&fakeInvocationContext{Context: context.Background(), sessionID: "sess-other"})

	if got := plugin.HeaderProvider(toolCall)["Authorization"]; got != "Bearer delegated-opaque" {
		t.Fatalf("the in-flight run lost its credential to another run's sweep, Authorization = %q", got)
	}
}

// An entry no run is using still ages out, so the eviction bound keeps bounding
// memory.
func TestSweepEvictsAnIdleEntryWithoutExpiry(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	plugin.setCachedToken("sess-idle", "alice", "opaque-token", 0)

	due := time.Now().Add(-time.Minute).Unix()
	plugin.tokenCache[cacheKey{sessionID: "sess-idle", subject: "alice"}].EvictAfter = due
	plugin.earliestEviction = due

	plugin.AfterRunCallback(&fakeInvocationContext{Context: context.Background(), sessionID: "sess-idle"})

	if _, ok := plugin.tokenCache[cacheKey{sessionID: "sess-idle", subject: "alice"}]; ok {
		t.Fatal("an entry no run is using must age out")
	}
}

// A burst of distinct credentials is capped, and the cap drops the least recently
// used entries rather than the one a run is still making tool calls with.
func TestCapacityBoundKeepsTheEntryOfAnInFlightCaller(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	const sessionID = "sess-burst"
	const bearer = "opaque-alice"
	plugin.setCachedToken(sessionID, subjectKey(bearer), "delegated-alice", 0)

	ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
	toolCall := fakeSessionContext{Context: ctx, sessionID: sessionID}

	const overflow = 25
	for i := range maxCacheEntries + overflow {
		// Each new caller is more recent than the last tool call of the
		// in-flight run, so only a renewed entry survives the burst.
		if got := plugin.HeaderProvider(toolCall)["Authorization"]; got != "Bearer delegated-alice" {
			t.Fatalf("in-flight tool call %d got Authorization %q", i, got)
		}
		plugin.setCachedToken(sessionID, subjectKey(fmt.Sprintf("token-%d", i)), "delegated", 0)
	}

	if len(plugin.tokenCache) != maxCacheEntries {
		t.Fatalf("cache holds %d entries, want the %d cap", len(plugin.tokenCache), maxCacheEntries)
	}
	if got := plugin.HeaderProvider(toolCall)["Authorization"]; got != "Bearer delegated-alice" {
		t.Fatalf("the in-flight run lost its credential to the capacity bound, Authorization = %q", got)
	}
	// The cap drops by use order, so the callers the burst left untouched
	// longest are exactly the ones gone.
	for i := range overflow + 1 {
		key := cacheKey{sessionID: sessionID, subject: subjectKey(fmt.Sprintf("token-%d", i))}
		if _, ok := plugin.tokenCache[key]; ok {
			t.Fatalf("token-%d is still cached, want the least recently used entries dropped", i)
		}
	}
}

// The sweep must evict entries belonging to subjects and sessions other than the
// acting one, since only the acting subject's key is derivable in the callback.
func TestAfterRunCallbackEvictsExpiredEntriesOfOtherSubjects(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
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
	if plugin.earliestEviction != future {
		t.Fatalf("earliestEviction = %d, want %d after the sweep", plugin.earliestEviction, future)
	}
}

// The earliest expiry gates the walk, so a cache with nothing evictable is left
// untouched instead of being traversed on every run.
func TestAfterRunCallbackSkipsWalkUntilSomethingExpires(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	future := time.Now().Add(time.Hour).Unix()
	plugin.setCachedToken("sess-a", "alice", "alice-token", future)

	plugin.AfterRunCallback(&fakeInvocationContext{Context: context.Background(), sessionID: "sess-a"})

	if _, ok := plugin.getCachedToken("sess-a", "alice"); !ok {
		t.Fatal("unexpired entry must survive")
	}
	if plugin.earliestEviction != future {
		t.Fatalf("earliestEviction = %d, want it left at %d", plugin.earliestEviction, future)
	}
}

func TestClearCacheResetsEarliestEviction(t *testing.T) {
	t.Parallel()

	plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)
	plugin.setCachedToken("sess-a", "alice", "alice-token", time.Now().Add(time.Hour).Unix())
	plugin.ClearCache()

	if plugin.earliestEviction != 0 {
		t.Fatalf("earliestEviction = %d, want 0 after ClearCache", plugin.earliestEviction)
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

// signedTokenExpiringIn mints a token whose exp claim is offset from now.
func signedTokenExpiringIn(t *testing.T, sub string, d time.Duration) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": "https://issuer.example",
		"sub": sub,
		"exp": time.Now().Add(d).Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return token
}

// The entry is keyed by the caller's credential, so it must not outlive it.
// Otherwise a caller replaying an expired bearer keeps hitting a cached
// delegated token instead of reaching the STS that would reject it.
func TestCachedEntryDoesNotOutliveTheCallerCredential(t *testing.T) {
	t.Parallel()

	// A delegated token that long outlives the caller's own credential.
	integration := newSTSIntegration(t, staticActor("actor"), func(*http.Request) map[string]any {
		resp := issued("long-lived")
		resp["expires_in"] = 3600
		return resp
	})

	plugin := NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)

	const sessionID = "sess-ttl"
	bearer := signedTokenExpiringIn(t, "alice", 30*time.Second)
	ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
	if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: ctx, sessionID: sessionID}); err != nil {
		t.Fatalf("BeforeRunCallback() error = %v", err)
	}

	entry, ok := plugin.getCachedToken(sessionID, subjectKey(bearer))
	if !ok {
		t.Fatal("expected a cached entry after the exchange")
	}
	if want := extractJWTExpiry(bearer); entry.Expiry != want {
		t.Fatalf("entry expiry = %d, want the caller credential's exp %d", entry.Expiry, want)
	}
}

// The entry's expiry is capped at the caller credential's own exp, so an expired
// entry means the caller's authority is gone. AfterRunCallback only sweeps
// between runs, so HeaderProvider has to reject such an entry itself.
func TestHeaderProviderRejectsEntryPastTheCallerExpiry(t *testing.T) {
	t.Parallel()

	integration := newSTSIntegration(t, staticActor("actor"), func(*http.Request) map[string]any {
		resp := issued("long-lived")
		resp["expires_in"] = 3600
		return resp
	})

	plugin := NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)

	const sessionID = "sess-expired-caller"
	bearer := signedTokenExpiringIn(t, "alice", 30*time.Second)
	ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
	if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: ctx, sessionID: sessionID}); err != nil {
		t.Fatalf("BeforeRunCallback() error = %v", err)
	}

	headerCtx := fakeSessionContext{Context: ctx, sessionID: sessionID}
	if got := plugin.HeaderProvider(headerCtx)["Authorization"]; got != "Bearer long-lived" {
		t.Fatalf("Authorization header = %q, want the delegated token", got)
	}

	plugin.tokenCache[cacheKey{sessionID: sessionID, subject: subjectKey(bearer)}].Expiry = time.Now().Unix() - 1
	if headers := plugin.HeaderProvider(headerCtx); headers != nil {
		t.Fatalf("HeaderProvider() = %v, want no headers once the caller credential expired", headers)
	}
}

// An exchange the STS rejects must not leave the previous delegated token
// injectable. The cache lookup precedes the exchange, so an exchange only runs
// once the entry is already unusable, and HeaderProvider applies the same check.
func TestFailedExchangeLeavesNoUsableEntry(t *testing.T) {
	t.Parallel()

	reject := false
	integration := newSTSIntegrationWithTokenHandler(t, staticActor("actor"), func(w http.ResponseWriter, _ *http.Request) {
		if reject {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		_ = json.NewEncoder(w).Encode(issued("exchanged-alice"))
	})

	plugin := NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)

	const sessionID = "sess-rejected"
	bearer := signedTokenWithSub(t, "alice")
	ctx := context.WithValue(context.Background(), kagentmodels.BearerTokenKey, bearer)
	if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: ctx, sessionID: sessionID}); err != nil {
		t.Fatalf("BeforeRunCallback() error = %v", err)
	}

	headerCtx := fakeSessionContext{Context: ctx, sessionID: sessionID}
	if got := plugin.HeaderProvider(headerCtx)["Authorization"]; got != "Bearer exchanged-alice" {
		t.Fatalf("Authorization header = %q, want the delegated token", got)
	}

	// Age the entry out, then have the STS reject the refresh.
	plugin.tokenCache[cacheKey{sessionID: sessionID, subject: subjectKey(bearer)}].Expiry = time.Now().Unix() - 1
	reject = true
	if _, err := plugin.BeforeRunCallback(&fakeInvocationContext{Context: ctx, sessionID: sessionID}); err != nil {
		t.Fatalf("BeforeRunCallback() error = %v", err)
	}

	if headers := plugin.HeaderProvider(headerCtx); headers != nil {
		t.Fatalf("HeaderProvider() = %v, want no headers after the exchange was rejected", headers)
	}
}

// earlierExpiry decides the cached entry's lifetime, so it must treat a missing
// expiry as "unknown" rather than as the epoch.
func TestEarlierExpiry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   int64
		candidate int64
		want      int64
	}{
		{name: "the candidate expires first", current: 200, candidate: 100, want: 100},
		{name: "the current entry expires first", current: 100, candidate: 200, want: 100},
		{name: "no candidate expiry keeps the current one", current: 100, candidate: 0, want: 100},
		{name: "no current expiry takes the candidate", current: 0, candidate: 100, want: 100},
		{name: "neither expires", current: 0, candidate: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := earlierExpiry(tt.current, tt.candidate); got != tt.want {
				t.Fatalf("earlierExpiry(%d, %d) = %d, want %d", tt.current, tt.candidate, got, tt.want)
			}
		})
	}
}
