package sts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"google.golang.org/adk/v2/agent"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/genai"
)

// maxCacheTTL bounds an entry whose token carries no usable expiry. The cache
// holds one entry per (session, subject), so a token without an expiry would
// otherwise pin an entry per caller for the lifetime of the process.
const maxCacheTTL = 5 * time.Minute

// TokenCacheEntry holds a cached token with its expiry time.
//
// Expiry 0 means the entry never expires. Only the actor token cache stores
// such entries; setCachedToken bounds every subject entry, because the subject
// cache holds one entry per caller rather than one per session.
type TokenCacheEntry struct {
	Token  string
	Expiry int64 // Unix timestamp, 0 if no expiry
}

// HasExpired checks if the token has expired or will expire soon.
func (e *TokenCacheEntry) HasExpired(bufferSeconds int64) bool {
	if e.Expiry == 0 {
		return false
	}
	return e.Expiry <= time.Now().Unix()+bufferSeconds
}

// TokenPropagationPlugin propagates STS tokens to ADK tools.
// It registers as a Go ADK plugin for run-level token preparation and exposes
// a header provider used by MCP tool transports.
type TokenPropagationPlugin struct {
	integration     *STSIntegration
	tokenCache      map[cacheKey]*TokenCacheEntry
	actorTokenCache *TokenCacheEntry // used only for dynamic fetchActorToken providers
	mu              sync.RWMutex
	logger          logr.Logger
	bufferSeconds   int64
	earliestExpiry  int64    // lower bound on the earliest Expiry in tokenCache; 0 when nothing is evictable
	resource        []string // RFC 8707 resource indicators sent on the STS exchange; empty omits them
	audience        []string // RFC 8693 audiences sent on the STS exchange; empty omits them
}

// NewTokenPropagationPlugin creates a new token propagation plugin.
// If integration is nil, the plugin will pass through tokens without exchange.
// resource and audience scope the exchanged token to a backend; empty values
// are omitted from the request, leaving the exchange unscoped.
func NewTokenPropagationPlugin(integration *STSIntegration, logger logr.Logger, resource, audience []string) *TokenPropagationPlugin {
	return &TokenPropagationPlugin{
		integration:   integration,
		tokenCache:    make(map[cacheKey]*TokenCacheEntry),
		logger:        logger.WithName("sts-plugin"),
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

// actingCredential returns the credential this request authenticates with, which
// is also what the cache key is derived from. models.BearerTokenFromContext
// prefers the value executor.withBearerToken stored and falls back to the A2A
// call context, which is what reaches the MCP transport layer: the call context
// is the same source the round-tripper's propagateToken path reads, so the
// per-subject key stays derivable even when BearerTokenKey is not threaded to
// the MCP request context.
func actingCredential(ctx context.Context) string {
	return models.BearerTokenFromContext(ctx)
}

// cacheKey scopes a cache entry to both the session and the acting subject so a
// session that carries messages from multiple subjects keeps one exchanged
// token per subject rather than collapsing to whichever arrived first.
type cacheKey struct {
	sessionID string
	subject   string
}

// getCachedToken retrieves a valid cached token for the session and subject.
func (p *TokenPropagationPlugin) getCachedToken(sessionID, subject string) (*TokenCacheEntry, bool) {
	// An empty subject identifies no principal, so it must never match an entry.
	if subject == "" {
		return nil, false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	entry, ok := p.tokenCache[cacheKey{sessionID: sessionID, subject: subject}]
	if !ok {
		return nil, false
	}

	if entry.HasExpired(p.bufferSeconds) {
		return nil, false
	}

	return entry, true
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

	// Every subject entry carries an expiry so the sweep can always evict it.
	// Only entries whose token has no usable exp fall back to maxCacheTTL;
	// a token that states its own expiry keeps it.
	if expiry == 0 {
		expiry = time.Now().Add(maxCacheTTL).Unix()
	}

	p.tokenCache[cacheKey{sessionID: sessionID, subject: subject}] = &TokenCacheEntry{
		Token:  token,
		Expiry: expiry,
	}
	if p.earliestExpiry == 0 || expiry < p.earliestExpiry {
		p.earliestExpiry = expiry
	}
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
		p.logger.V(1).Info("No session ID available, skipping token propagation")
		return nil, nil
	}

	// Resolve the acting credential before the cache lookup: the cache is keyed by
	// the acting subject, and a session shared by multiple subjects would otherwise
	// reuse the first caller's token for every later caller.
	bearerToken := actingCredential(ctx)

	if bearerToken == "" {
		p.logger.V(1).Info("No bearer token in context, skipping token propagation", "sessionID", sessionID)
		return nil, nil
	}

	subject := subjectKey(bearerToken)

	// Check if we already have a valid cached token for this session and subject.
	if entry, ok := p.getCachedToken(sessionID, subject); ok {
		p.logger.V(1).Info("Using cached STS token", "sessionID", sessionID,
			"expiresIn", time.Until(time.Unix(entry.Expiry, 0)).String())
		return nil, nil
	}

	// Get subject token
	subjectToken := bearerToken
	if p.integration != nil {
		subjectToken = p.integration.GetSubjectToken(bearerToken)
	}

	if subjectToken == "" {
		p.logger.V(1).Info("Empty subject token extracted, skipping", "sessionID", sessionID)
		return nil, nil
	}

	if p.integration != nil {
		actorToken, err := p.actorTokenForExchange(ctx)
		if err != nil {
			p.logger.Error(err, "Failed to fetch actor token dynamically, skipping STS token exchange", "sessionID", sessionID)
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
			p.logger.Error(err, "STS token exchange failed, tools may not authenticate", "sessionID", sessionID)
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
		// it: replaying an expired bearer would otherwise keep hitting a cached
		// delegated token instead of reaching the STS.
		expiry = earlierExpiry(expiry, extractJWTExpiry(bearerToken))
		p.setCachedToken(sessionID, subject, exchangedToken, expiry)
		p.logger.Info("Successfully exchanged and cached STS token", "sessionID", sessionID)
	} else {
		// No STS integration — cache the raw subject token for header injection.
		expiry := earlierExpiry(extractJWTExpiry(subjectToken), extractJWTExpiry(bearerToken))
		p.setCachedToken(sessionID, subject, subjectToken, expiry)
		p.logger.V(1).Info("Cached subject token (no STS exchange)", "sessionID", sessionID)
	}

	return nil, nil
}

// AfterRunCallback is called after the ADK run finishes.
// It cleans up expired tokens from the cache.
func (p *TokenPropagationPlugin) AfterRunCallback(_ agent.InvocationContext) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// A session holds one entry per subject and only the acting subject's key is
	// derivable here, so the sweep covers every entry rather than the caller's
	// alone; scoping it to the current session would strand the entries of
	// sessions that never run again. The earliest expiry gates the walk, so a
	// large cache is only traversed once something can actually be evicted.
	if p.earliestExpiry != 0 && p.earliestExpiry <= time.Now().Unix()+p.bufferSeconds {
		earliest := int64(0)
		for key, entry := range p.tokenCache {
			if entry.HasExpired(p.bufferSeconds) {
				delete(p.tokenCache, key)
				continue
			}
			if earliest == 0 || entry.Expiry < earliest {
				earliest = entry.Expiry
			}
		}
		p.earliestExpiry = earliest
	}

	if p.actorTokenCache != nil && p.actorTokenCache.HasExpired(p.bufferSeconds) {
		p.logger.V(1).Info("Removing expired actor token from cache")
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
		p.logger.V(1).Info("No session ID in context, MCP request will use existing headers")
		return nil
	}

	// Derive the acting subject from this request's own credential, so the injected
	// token matches the caller of this request rather than whichever subject
	// first seeded the session.
	subject := subjectKey(actingCredential(ctx))

	entry, ok := p.getCachedToken(sessionID, subject)
	if !ok {
		p.logger.V(1).Info("No cached STS token for session/subject, MCP request will use existing headers", "sessionID", sessionID)
		return nil
	}

	p.logger.V(1).Info("Injecting STS token into MCP request headers", "sessionID", sessionID)
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
	p.earliestExpiry = 0
	p.actorTokenCache = nil
	p.logger.Info("Cleared STS token cache")
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
