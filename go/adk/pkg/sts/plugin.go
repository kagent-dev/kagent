package sts

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"log/slog"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"google.golang.org/adk/v2/agent"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/genai"
)

// evictAfterIdle bounds an entry whose tokens state no expiry, so that such an
// entry does not pin a slot for the lifetime of the process. It bounds memory
// only, never the caller's authority, and a cache hit renews it.
const evictAfterIdle = 5 * time.Minute

// maxEntriesPerSession bounds the entries one session may hold. A session
// carries as many entries as it has distinct callers, so the bound is what
// keeps one session's burst of credentials from evicting another session's.
const maxEntriesPerSession = 32

// maxCacheEntries bounds the whole cache, across sessions. The sweep runs
// between runs, so concurrent runs holding many distinct credentials need a
// bound that does not wait for one.
const maxCacheEntries = 1024

// TokenCacheEntry holds a cached token with its expiry time.
type TokenCacheEntry struct {
	Token string
	// Expiry is when the authority behind the token ends, 0 when nothing on
	// this path states one. It decides whether the token may still be injected.
	Expiry int64
	// evictAfter is when the entry may be dropped. It bounds memory only, never
	// the caller's authority, so it can come due before Expiry. touch sets it
	// when nothing states an expiry.
	evictAfter int64
	// useSeq orders the capacity bounds. It is a sequence number, not a time.
	useSeq uint64
}

// HasExpired checks if the token has expired or will expire soon.
func (e *TokenCacheEntry) HasExpired(bufferSeconds int64) bool {
	return hasPassed(e.Expiry, bufferSeconds)
}

// evictable reports whether the entry may be dropped to bound memory.
func (e *TokenCacheEntry) evictable(bufferSeconds int64) bool {
	return hasPassed(e.evictAfter, bufferSeconds)
}

// hasPassed reports whether a deadline has come due; 0 means there is none.
func hasPassed(deadline, bufferSeconds int64) bool {
	if deadline == 0 {
		return false
	}
	return deadline <= time.Now().Unix()+bufferSeconds
}

// TokenPropagationPlugin propagates STS tokens to ADK tools.
// It registers as a Go ADK plugin for run-level token preparation and exposes
// a header provider used by MCP tool transports.
type TokenPropagationPlugin struct {
	integration     *STSIntegration
	tokenCache      map[cacheKey]*TokenCacheEntry
	actorTokenCache *TokenCacheEntry // used only for dynamic fetchActorToken providers
	mu              sync.RWMutex
	logger          *slog.Logger
	bufferSeconds   int64
	uses            uint64   // monotonic use counter, read by touch
	resource        []string // RFC 8707 resource indicators sent on the STS exchange; empty omits them
	audience        []string // RFC 8693 audiences sent on the STS exchange; empty omits them
}

// NewTokenPropagationPlugin creates a new token propagation plugin.
// If integration is nil, the plugin will pass through tokens without exchange.
// resource and audience scope the exchanged token to a backend; empty values
// are omitted from the request, leaving the exchange unscoped.
func NewTokenPropagationPlugin(integration *STSIntegration, logger *slog.Logger, resource, audience []string) *TokenPropagationPlugin {
	return &TokenPropagationPlugin{
		integration:   integration,
		tokenCache:    make(map[cacheKey]*TokenCacheEntry),
		logger:        logger.With("component", "sts-plugin"),
		bufferSeconds: 5,
		resource:      resource,
		audience:      audience,
	}
}

// earlierExpiry returns the earlier of two Unix expiry timestamps, treating 0
// as "no expiry known" rather than as the epoch.
func earlierExpiry(current, candidate int64) int64 {
	if candidate == 0 {
		return current
	}
	if current == 0 || candidate < current {
		return candidate
	}
	return current
}

// subjectKey derives a per-principal cache discriminator from a bearer token: a
// hash of the raw token.
//
// A cache hit hands the caller a delegated token without performing an exchange,
// so the key decides who receives someone else's authority. Deriving it from
// unverified "iss"/"sub" claims would let a forged, unsigned token select a
// victim's entry and never reach the STS that would have rejected it. Hashing
// the raw token instead makes a forged token a cache miss, so it goes to the
// STS and fails there.
//
// The cost is a re-exchange when a principal's bearer rotates mid-session.
func subjectKey(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// cacheKey scopes a cache entry to both the session and the acting subject so a
// session that carries messages from multiple subjects keeps one exchanged
// token per subject rather than collapsing to whichever arrived first.
type cacheKey struct {
	sessionID string
	subject   string
}

// getCachedToken retrieves a valid cached token for the session and subject. It
// returns a copy, so no caller holds the cached entry once the lock is released
// and a concurrent touch cannot write fields a caller is reading.
func (p *TokenPropagationPlugin) getCachedToken(sessionID, subject string) (TokenCacheEntry, bool) {
	// An empty subject identifies no principal, so it must never match an entry.
	if subject == "" {
		return TokenCacheEntry{}, false
	}

	// A write lock, not a read lock: a hit records the use, which is what keeps
	// the sweep and the capacity bounds off an entry a run is still using.
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.tokenCache[cacheKey{sessionID: sessionID, subject: subject}]
	if !ok {
		return TokenCacheEntry{}, false
	}

	// Tested on Expiry, never on evictAfter: the entry is capped at the caller
	// credential's own expiry, so an expired one means the caller's authority is
	// gone. The sweep runs between runs, so the check belongs here too.
	if entry.HasExpired(p.bufferSeconds) {
		return TokenCacheEntry{}, false
	}

	p.touch(entry)
	return *entry, true
}

// touch records that a run is using an entry. It must be called with p.mu held.
//
// Every bound on the cache drops the entries no run is using: the capacity
// bounds by use order, the sweep by eviction time. An entry whose tokens state
// no expiry carries a synthetic eviction time, so a use renews it. Without
// this, a concurrent run's sweep drops the credential of a run that is still in
// flight, which BeforeRunCallback runs too late to re-mint.
func (p *TokenPropagationPlugin) touch(entry *TokenCacheEntry) {
	p.uses++
	entry.useSeq = p.uses
	if entry.Expiry == 0 {
		entry.evictAfter = time.Now().Add(evictAfterIdle).Unix()
	}
}

// evictOverSessionCapacity drops the least recently used entries of one session
// once that session is over capacity. It must be called with p.mu held.
//
// The bound is per session so that a session presenting many distinct
// credentials evicts its own entries and not those of unrelated sessions.
func (p *TokenPropagationPlugin) evictOverSessionCapacity(sessionID string) {
	// The whole cache is walked because entries are not indexed by session, but
	// only the keys of this session are collected, and only once the session can
	// be over capacity at all.
	if len(p.tokenCache) <= maxEntriesPerSession {
		return
	}

	keys := make([]cacheKey, 0, maxEntriesPerSession+1)
	for key := range p.tokenCache {
		if key.sessionID == sessionID {
			keys = append(keys, key)
		}
	}
	p.evictLeastRecentlyUsed(keys, len(keys)-maxEntriesPerSession, "session")
}

// evictOverCapacity drops the least recently used entries once the whole cache
// is over capacity. It must be called with p.mu held.
//
// The sweep runs between runs only, so it cannot bound runs that see many
// distinct credentials while they hold them.
func (p *TokenPropagationPlugin) evictOverCapacity() {
	overflow := len(p.tokenCache) - maxCacheEntries
	if overflow <= 0 {
		return
	}

	keys := make([]cacheKey, 0, len(p.tokenCache))
	for key := range p.tokenCache {
		keys = append(keys, key)
	}
	p.evictLeastRecentlyUsed(keys, overflow, "cache")
}

// evictLeastRecentlyUsed drops the overflow least recently used entries among
// keys. It must be called with p.mu held.
//
// Unlike the sweep, this drops entries whose credentials are still valid, so a
// run that is between tool calls can lose the token it would have injected next.
// It is reported above debug for that reason. The entry the caller just cached
// is the most recently used, so it is never dropped here.
func (p *TokenPropagationPlugin) evictLeastRecentlyUsed(keys []cacheKey, overflow int, bound string) {
	if overflow <= 0 {
		return
	}

	slices.SortFunc(keys, func(a, b cacheKey) int {
		return cmp.Compare(p.tokenCache[a].useSeq, p.tokenCache[b].useSeq)
	})
	for _, key := range keys[:overflow] {
		delete(p.tokenCache, key)
	}
	p.logger.Warn("dropped valid cached tokens to stay within capacity; callers may re-exchange",
		"bound", bound, "dropped", overflow)
}

// setCachedToken caches a token for the session and subject.
func (p *TokenPropagationPlugin) setCachedToken(sessionID, subject, token string, expiry int64) {
	// An empty subject identifies no principal, so an entry stored under it would
	// be shared by every credential-less caller in the session.
	if subject == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// evictAfter defaults to Expiry; touch supplies the synthetic bound when
	// nothing states an expiry, so every entry stays evictable.
	entry := &TokenCacheEntry{Token: token, Expiry: expiry, evictAfter: expiry}
	p.tokenCache[cacheKey{sessionID: sessionID, subject: subject}] = entry
	p.touch(entry)

	p.evictOverSessionCapacity(sessionID)
	p.evictOverCapacity()
}

func (p *TokenPropagationPlugin) getCachedActorToken() (*TokenCacheEntry, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.actorTokenCache == nil || p.actorTokenCache.HasExpired(p.bufferSeconds) {
		return nil, false
	}
	return p.actorTokenCache, true
}

func (p *TokenPropagationPlugin) setCachedActorToken(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.actorTokenCache = &TokenCacheEntry{
		Token:  token,
		Expiry: extractJWTExpiry(token),
	}
}

func (p *TokenPropagationPlugin) actorTokenForExchange(ctx context.Context) (string, error) {
	if p.integration == nil {
		return "", nil
	}

	if p.integration.fetchActorToken == nil {
		return p.integration.actorTokenForExchange(ctx)
	}

	if entry, ok := p.getCachedActorToken(); ok {
		return entry.Token, nil
	}

	actorToken, err := p.integration.actorTokenForExchange(ctx)
	if err != nil || actorToken == "" {
		return actorToken, err
	}

	p.setCachedActorToken(actorToken)
	return actorToken, nil
}

// BeforeRunCallback is called before the ADK run starts.
// It extracts the subject token, performs STS exchange if needed, and caches the result.
func (p *TokenPropagationPlugin) BeforeRunCallback(ctx agent.InvocationContext) (*genai.Content, error) {
	sessionID := ""
	if session := ctx.Session(); session != nil {
		sessionID = session.ID()
	}
	if sessionID == "" {
		p.logger.Debug("no session ID available, skipping token propagation")
		return nil, nil
	}

	// Resolve the acting credential before the cache lookup: the cache is keyed by
	// the acting subject, and a session shared by multiple subjects would otherwise
	// reuse the first caller's token for every later caller.
	bearerToken := models.BearerTokenFromContext(ctx)

	if bearerToken == "" {
		p.logger.Debug("no bearer token in context, skipping token propagation", "session_id", sessionID)
		return nil, nil
	}

	subject := subjectKey(bearerToken)

	// Check if we already have a valid cached token for this session and subject.
	if entry, ok := p.getCachedToken(sessionID, subject); ok {
		// Nothing on this path states an expiry for an opaque credential, and
		// time.Unix(0, 0) would report the epoch as a deadline decades past.
		if entry.Expiry == 0 {
			p.logger.Debug("using cached STS token", "session_id", sessionID)
		} else {
			p.logger.Debug("using cached STS token", "session_id", sessionID,
				"expires_in", time.Until(time.Unix(entry.Expiry, 0)).String())
		}
		return nil, nil
	}

	// Get subject token
	subjectToken := bearerToken
	if p.integration != nil {
		subjectToken = p.integration.GetSubjectToken(bearerToken)
	}

	if subjectToken == "" {
		p.logger.Debug("empty subject token extracted, skipping", "session_id", sessionID)
		return nil, nil
	}

	if p.integration != nil {
		actorToken, err := p.actorTokenForExchange(ctx)
		if err != nil {
			p.logger.Error("failed to fetch actor token dynamically, skipping STS token exchange", "error", err, "session_id", sessionID)
			return nil, nil
		}

		resp, err := p.integration.ExchangeTokenWithActorToken(
			ctx,
			subjectToken,
			TokenTypeJWT,
			actorToken,
			p.resource,
			p.audience,
			"", // scope
			"", // requestedTokenType
		)
		if err != nil {
			p.logger.Error("STS token exchange failed, tools may not authenticate", "error", err, "session_id", sessionID)
			return nil, nil
		}

		// Cache the exchanged token.
		exchangedToken := resp.AccessToken
		expiry := int64(0)
		if resp.ExpiresIn > 0 {
			expiry = time.Now().Unix() + int64(resp.ExpiresIn)
		} else {
			// Fall back to JWT exp claim for cache TTL.
			expiry = extractJWTExpiry(exchangedToken)
		}
		// The entry is keyed by the caller's credential, so it must not outlive
		// it, nor the subject token the exchange rests on: replaying an expired
		// credential would otherwise keep hitting a cached delegated token
		// instead of reaching the STS.
		expiry = earlierExpiry(expiry, extractJWTExpiry(subjectToken))
		expiry = earlierExpiry(expiry, extractJWTExpiry(bearerToken))
		p.setCachedToken(sessionID, subject, exchangedToken, expiry)
		p.logger.Info("successfully exchanged and cached STS token", "session_id", sessionID)
	} else {
		// No STS integration — cache the raw subject token for header injection.
		expiry := earlierExpiry(extractJWTExpiry(subjectToken), extractJWTExpiry(bearerToken))
		p.setCachedToken(sessionID, subject, subjectToken, expiry)
		p.logger.Debug("cached subject token (no STS exchange)", "session_id", sessionID)
	}

	return nil, nil
}

// AfterRunCallback is called after the ADK run finishes.
// It cleans up expired tokens from the cache.
func (p *TokenPropagationPlugin) AfterRunCallback(_ agent.InvocationContext) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// The sweep covers every entry rather than the caller's alone; scoping it to
	// the current session would strand the entries of sessions that never run
	// again. A use renews the eviction time, so this cannot drop the credential
	// of a run that is still in flight. The capacity bounds hold the walk to
	// maxCacheEntries, and a run spans model calls, so it is not worth gating.
	for key, entry := range p.tokenCache {
		if entry.evictable(p.bufferSeconds) {
			delete(p.tokenCache, key)
		}
	}

	if p.actorTokenCache != nil && p.actorTokenCache.HasExpired(p.bufferSeconds) {
		p.logger.Debug("removing expired actor token from cache")
		p.actorTokenCache = nil
	}
}

// HeaderProvider returns a map of headers to inject into MCP tool HTTP requests.
// It is called by the dynamicHeaderRoundTripper on every MCP HTTP request.
func (p *TokenPropagationPlugin) HeaderProvider(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}

	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		p.logger.DebugContext(ctx, "no session ID in context, MCP request will use existing headers")
		return nil
	}

	// Derive the acting subject from this request's own credential, so the injected
	// token matches the caller of this request rather than whichever subject
	// first seeded the session. BearerTokenFromContext falls back to the A2A call
	// context, the same source the round-tripper's propagateToken path reads, so
	// the key stays derivable here even when BearerTokenKey was not threaded into
	// the MCP request context.
	subject := subjectKey(models.BearerTokenFromContext(ctx))
	if subject == "" {
		p.logger.DebugContext(ctx, "no caller credential on the request, MCP request will use existing headers", "session_id", sessionID)
		return nil
	}

	entry, ok := p.getCachedToken(sessionID, subject)
	if !ok {
		// The caller is identified but has no usable entry, so this request loses
		// its delegated identity. Reported above debug: nothing else says so.
		p.logger.WarnContext(ctx, "no valid cached STS token for this caller, MCP request will use existing headers", "session_id", sessionID)
		return nil
	}

	p.logger.DebugContext(ctx, "injecting STS token into MCP request headers", "session_id", sessionID)
	return map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", entry.Token),
	}
}

// Extract session ID from ADK tool / invocation context, which implements SessionID().
func sessionIDFromContext(ctx context.Context) string {
	type sessionContext interface {
		SessionID() string
	}
	sessionCtx, ok := ctx.(sessionContext)
	if !ok {
		return ""
	}
	return sessionCtx.SessionID()
}

// ClearCache clears all cached tokens.
func (p *TokenPropagationPlugin) ClearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tokenCache = make(map[cacheKey]*TokenCacheEntry)
	p.actorTokenCache = nil
	p.logger.Info("cleared STS token cache")
}

// ADKPlugin returns the Go ADK plugin registered with runner.PluginConfig.
func (p *TokenPropagationPlugin) ADKPlugin() (*adkplugin.Plugin, error) {
	return adkplugin.New(adkplugin.Config{
		Name:              "kagent-sts-token-propagation",
		BeforeRunCallback: p.BeforeRunCallback,
		AfterRunCallback:  p.AfterRunCallback,
	})
}

// extractJWTExpiry extracts the 'exp' claim from a JWT token without verifying
// its signature. This is ONLY used for cache TTL management, not for security
// decisions. Token validation happens server-side during STS exchange.
func extractJWTExpiry(token string) int64 {
	if token == "" {
		return 0
	}
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser(jwt.WithoutClaimsValidation()).ParseUnverified(token, claims); err != nil {
		return 0
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return 0
	}
	return exp.Unix()
}
