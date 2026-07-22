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

// TokenCacheEntry holds a cached token with its expiry time.
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
}

// NewTokenPropagationPlugin creates a new token propagation plugin.
// If integration is nil, the plugin will pass through tokens without exchange.
func NewTokenPropagationPlugin(integration *STSIntegration, logger logr.Logger) *TokenPropagationPlugin {
	return &TokenPropagationPlugin{
		integration:   integration,
		tokenCache:    make(map[cacheKey]*TokenCacheEntry),
		logger:        logger.WithName("sts-plugin"),
		bufferSeconds: 5,
	}
}

// subjectKey derives a stable per-principal cache discriminator from a bearer
// token: the "sub" claim when present, otherwise a hash of the raw token so
// opaque or sub-less tokens still partition per principal. The token is parsed
// unverified — this only partitions the cache and never gates a security
// decision; the token is validated server-side during STS exchange.
func subjectKey(token string) string {
	if token == "" {
		return ""
	}
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser(jwt.WithoutClaimsValidation()).ParseUnverified(token, claims); err == nil {
		if sub, ok := claims["sub"].(string); ok && sub != "" {
			return sub
		}
	}
	sum := sha256.Sum256([]byte(token))
	return "h:" + hex.EncodeToString(sum[:])
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
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tokenCache[cacheKey{sessionID: sessionID, subject: subject}] = &TokenCacheEntry{
		Token:  token,
		Expiry: expiry,
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

	// Extract bearer token from context. executor.go stores it with models.BearerTokenKey.
	// This must happen before the cache lookup: the cache is keyed by the acting
	// subject, and a session shared by multiple subjects would otherwise reuse the
	// first caller's token for every later caller.
	bearerToken := ""
	if v := ctx.Value(models.BearerTokenKey); v != nil {
		if token, ok := v.(string); ok {
			bearerToken = token
		}
	}

	if bearerToken == "" {
		p.logger.V(1).Info("No bearer token in context, skipping token propagation", "sessionID", sessionID)
		return nil, nil
	}

	subject := subjectKey(bearerToken)

	// Check if we already have a valid cached token for this session and subject.
	if entry, ok := p.getCachedToken(sessionID, subject); ok {
		p.logger.V(1).Info("Using cached STS token", "sessionID", sessionID)
		if entry.Expiry > 0 {
			p.logger.V(1).Info("Token expiry remaining",
				"expiresIn", time.Until(time.Unix(entry.Expiry, 0)).String())
		}
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
			nil, // resource
			nil, // audience
			"",  // scope
			"",  // requestedTokenType
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
		p.setCachedToken(sessionID, subject, exchangedToken, expiry)
		p.logger.Info("Successfully exchanged and cached STS token", "sessionID", sessionID)
	} else {
		// No STS integration — cache the raw subject token for header injection.
		expiry := extractJWTExpiry(subjectToken)
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

	for key, entry := range p.tokenCache {
		if entry.HasExpired(p.bufferSeconds) {
			delete(p.tokenCache, key)
		}
	}
	if p.actorTokenCache != nil && p.actorTokenCache.HasExpired(p.bufferSeconds) {
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

	// Recover the acting subject from the same bearer the executor stored, so the
	// injected token matches the caller of this request rather than whichever
	// subject first seeded the session.
	subject := ""
	if v := ctx.Value(models.BearerTokenKey); v != nil {
		if token, ok := v.(string); ok {
			subject = subjectKey(token)
		}
	}

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

// GetTokenForSession retrieves the cached token for a specific session and
// subject. Returns empty string if no valid token is cached.
func (p *TokenPropagationPlugin) GetTokenForSession(sessionID, subject string) string {
	entry, ok := p.getCachedToken(sessionID, subject)
	if !ok {
		return ""
	}
	return entry.Token
}

// ClearCache clears all cached tokens.
func (p *TokenPropagationPlugin) ClearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tokenCache = make(map[cacheKey]*TokenCacheEntry)
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

// extractJWTExpiry extracts the 'exp' claim from a JWT token without verifying its signature.
// This is ONLY used for cache TTL management, not for security decisions.
// Token validation happens server-side during STS exchange.
func extractJWTExpiry(token string) int64 {
	if token == "" {
		return 0
	}

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser(jwt.WithoutClaimsValidation()).ParseUnverified(token, claims); err != nil {
		return 0
	}

	if exp, ok := claims["exp"]; ok {
		switch v := exp.(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		}
	}

	return 0
}
