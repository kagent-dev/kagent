package sts

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"google.golang.org/adk/agent"
	adkplugin "google.golang.org/adk/plugin"
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
	tokenCache      map[string]*TokenCacheEntry // keyed by session ID
	actorTokenCache *TokenCacheEntry            // used only for dynamic fetchActorToken providers
	mu              sync.RWMutex
	logger          logr.Logger
	bufferSeconds   int64
}

// NewTokenPropagationPlugin creates a new token propagation plugin.
// If integration is nil, the plugin will pass through tokens without exchange.
func NewTokenPropagationPlugin(integration *STSIntegration, logger logr.Logger) *TokenPropagationPlugin {
	return &TokenPropagationPlugin{
		integration:   integration,
		tokenCache:    make(map[string]*TokenCacheEntry),
		logger:        logger.WithName("sts-plugin"),
		bufferSeconds: 5,
	}
}

// getCachedToken retrieves a valid cached token for the session.
func (p *TokenPropagationPlugin) getCachedToken(sessionID string) (*TokenCacheEntry, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entry, ok := p.tokenCache[sessionID]
	if !ok {
		return nil, false
	}

	if entry.HasExpired(p.bufferSeconds) {
		return nil, false
	}

	return entry, true
}

// setCachedToken caches a token for the session.
func (p *TokenPropagationPlugin) setCachedToken(sessionID string, token string, expiry int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tokenCache[sessionID] = &TokenCacheEntry{
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

	// Check if we already have a valid cached token for this session.
	if entry, ok := p.getCachedToken(sessionID); ok {
		p.logger.V(1).Info("Using cached STS token", "sessionID", sessionID)
		if entry.Expiry > 0 {
			p.logger.V(1).Info("Token expiry remaining",
				"expiresIn", time.Until(time.Unix(entry.Expiry, 0)).String())
		}
		return nil, nil
	}

	// Extract bearer token from context. executor.go stores it with models.BearerTokenKey.
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
		p.setCachedToken(sessionID, exchangedToken, expiry)
		p.logger.Info("Successfully exchanged and cached STS token", "sessionID", sessionID)
	} else {
		// No STS integration — cache the raw subject token for header injection.
		expiry := extractJWTExpiry(subjectToken)
		p.setCachedToken(sessionID, subjectToken, expiry)
		p.logger.V(1).Info("Cached subject token (no STS exchange)", "sessionID", sessionID)
	}

	return nil, nil
}

// AfterRunCallback is called after the ADK run finishes.
// It cleans up expired tokens from the cache.
func (p *TokenPropagationPlugin) AfterRunCallback(ctx agent.InvocationContext) {
	sessionID := ""
	if session := ctx.Session(); session != nil {
		sessionID = session.ID()
	}
	if sessionID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove expired subject token.
	if entry, ok := p.tokenCache[sessionID]; ok {
		if entry.HasExpired(p.bufferSeconds) {
			p.logger.V(1).Info("Removing expired subject token from cache", "sessionID", sessionID)
			delete(p.tokenCache, sessionID)
		}
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

	entry, ok := p.getCachedToken(sessionID)
	if !ok {
		p.logger.V(1).Info("No cached STS token for session, MCP request will use existing headers", "sessionID", sessionID)
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

// GetTokenForSession retrieves the cached token for a specific session.
// Returns empty string if no valid token is cached.
func (p *TokenPropagationPlugin) GetTokenForSession(sessionID string) string {
	entry, ok := p.getCachedToken(sessionID)
	if !ok {
		return ""
	}
	return entry.Token
}

// ClearCache clears all cached tokens.
func (p *TokenPropagationPlugin) ClearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tokenCache = make(map[string]*TokenCacheEntry)
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
