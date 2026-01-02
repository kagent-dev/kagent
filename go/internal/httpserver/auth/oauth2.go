/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

var (
	// ErrMissingToken is returned when no bearer token is provided
	ErrMissingToken = errors.New("missing bearer token")
	// ErrInvalidToken is returned when the token is invalid or expired
	ErrInvalidToken = errors.New("invalid or expired token")
	// ErrTokenValidation is returned when token validation fails
	ErrTokenValidation = errors.New("token validation failed")
	// ErrDiscoveryFailed is returned when OIDC discovery fails
	ErrDiscoveryFailed = errors.New("OIDC discovery failed")
)

// OAuth2Config contains configuration for the OAuth2 authenticator
type OAuth2Config struct {
	// IssuerURL is the OIDC issuer URL (e.g., https://accounts.google.com)
	IssuerURL string
	// ClientID is the OAuth2 client ID for token validation
	ClientID string
	// Audience is the expected audience claim in the token
	Audience string
	// RequiredScopes are the scopes that must be present in the token
	RequiredScopes []string
	// UserIDClaim is the claim to use as the user ID (default: "sub")
	UserIDClaim string
	// RolesClaim is the claim to use for roles (default: "roles")
	RolesClaim string
	// SkipIssuerValidation disables issuer validation (not recommended for production)
	SkipIssuerValidation bool
	// SkipExpiryValidation disables expiry validation (not recommended for production)
	SkipExpiryValidation bool
	// HTTPClient is an optional custom HTTP client for OIDC discovery
	HTTPClient *http.Client
	// JWKSCacheDuration is how long to cache the JWKS (default: 1 hour)
	JWKSCacheDuration time.Duration
}

// oidcDiscoveryDocument represents the OIDC discovery document
type oidcDiscoveryDocument struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
	ClaimsSupported       []string `json:"claims_supported,omitempty"`
}

// OAuth2Authenticator implements the auth.AuthProvider interface for OAuth2/OIDC
type OAuth2Authenticator struct {
	config     OAuth2Config
	httpClient *http.Client

	// JWKS cache
	jwksMu        sync.RWMutex
	jwksCache     jwk.Set
	jwksCacheTime time.Time

	// Discovery cache
	discoveryMu        sync.RWMutex
	discoveryCache     *oidcDiscoveryDocument
	discoveryCacheTime time.Time
}

// OAuth2Session implements the auth.Session interface
type OAuth2Session struct {
	principal   auth.Principal
	accessToken string
	claims      map[string]interface{}
	expiresAt   time.Time
}

func (s *OAuth2Session) Principal() auth.Principal {
	return s.principal
}

// Claims returns the raw JWT claims
func (s *OAuth2Session) Claims() map[string]interface{} {
	return s.claims
}

// AccessToken returns the original access token
func (s *OAuth2Session) AccessToken() string {
	return s.accessToken
}

// ExpiresAt returns when the session expires
func (s *OAuth2Session) ExpiresAt() time.Time {
	return s.expiresAt
}

// NewOAuth2Authenticator creates a new OAuth2 authenticator
func NewOAuth2Authenticator(config OAuth2Config) (*OAuth2Authenticator, error) {
	if config.IssuerURL == "" {
		return nil, fmt.Errorf("issuer URL is required")
	}

	// Set defaults
	if config.UserIDClaim == "" {
		config.UserIDClaim = "sub"
	}
	if config.RolesClaim == "" {
		config.RolesClaim = "roles"
	}
	if config.JWKSCacheDuration == 0 {
		config.JWKSCacheDuration = time.Hour
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &OAuth2Authenticator{
		config:     config,
		httpClient: httpClient,
	}, nil
}

// Authenticate validates the bearer token and returns a session
func (a *OAuth2Authenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	// Extract bearer token from Authorization header
	token := extractBearerToken(reqHeaders)
	if token == "" {
		// Also check query parameter for WebSocket connections
		token = query.Get("access_token")
	}
	if token == "" {
		return nil, ErrMissingToken
	}

	// Validate the token
	session, err := a.validateToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	return session, nil
}

// UpstreamAuth adds authentication to upstream requests
func (a *OAuth2Authenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	if session == nil {
		return nil
	}

	// Forward the user ID in a header
	if session.Principal().User.ID != "" {
		r.Header.Set("X-User-Id", session.Principal().User.ID)
	}

	// If we have an OAuth2Session, forward the access token
	if oauth2Session, ok := session.(*OAuth2Session); ok && oauth2Session.accessToken != "" {
		r.Header.Set("Authorization", "Bearer "+oauth2Session.accessToken)
	}

	return nil
}

// validateToken validates a JWT token and extracts claims
func (a *OAuth2Authenticator) validateToken(ctx context.Context, tokenString string) (*OAuth2Session, error) {
	// Get JWKS for validation
	keySet, err := a.getJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWKS: %w", err)
	}

	// Build validation options
	validateOpts := []jwt.ValidateOption{
		jwt.WithContext(ctx),
	}

	// If skipping expiry validation, reset default validators to avoid automatic exp check
	if a.config.SkipExpiryValidation {
		validateOpts = append(validateOpts, jwt.WithResetValidators(true))
	}

	if !a.config.SkipIssuerValidation && a.config.IssuerURL != "" {
		validateOpts = append(validateOpts, jwt.WithIssuer(a.config.IssuerURL))
	}

	if a.config.Audience != "" {
		validateOpts = append(validateOpts, jwt.WithAudience(a.config.Audience))
	}

	// Parse and validate the token
	// Disable automatic validation during parsing so we can control it
	parseOpts := []jwt.ParseOption{
		jwt.WithKeySet(keySet),
		jwt.WithValidate(false), // We'll validate manually after parsing
	}

	token, err := jwt.ParseString(tokenString, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// Validate the token
	if err := jwt.Validate(token, validateOpts...); err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	// Check required scopes if configured
	if len(a.config.RequiredScopes) > 0 {
		if err := a.validateScopes(token); err != nil {
			return nil, err
		}
	}

	// Extract claims for session
	claims := make(map[string]interface{})
	for iter := token.Iterate(ctx); iter.Next(ctx); {
		pair := iter.Pair()
		claims[pair.Key.(string)] = pair.Value
	}

	// Extract user ID
	userID := ""
	if idClaim, ok := claims[a.config.UserIDClaim]; ok {
		if id, ok := idClaim.(string); ok {
			userID = id
		}
	}

	// Extract roles
	var roles []string
	if rolesClaim, ok := claims[a.config.RolesClaim]; ok {
		switch r := rolesClaim.(type) {
		case []interface{}:
			for _, role := range r {
				if s, ok := role.(string); ok {
					roles = append(roles, s)
				}
			}
		case []string:
			roles = r
		case string:
			// Some providers return roles as a space-separated string
			roles = strings.Fields(r)
		}
	}

	// Get expiration time
	expiresAt := token.Expiration()

	return &OAuth2Session{
		principal: auth.Principal{
			User: auth.User{
				ID:    userID,
				Roles: roles,
			},
		},
		accessToken: tokenString,
		claims:      claims,
		expiresAt:   expiresAt,
	}, nil
}

// validateScopes checks if the token has all required scopes
func (a *OAuth2Authenticator) validateScopes(token jwt.Token) error {
	scopeClaim, ok := token.Get("scope")
	if !ok {
		// Try "scp" claim (used by some providers like Azure AD)
		scopeClaim, ok = token.Get("scp")
	}

	if !ok {
		return fmt.Errorf("token missing scope claim")
	}

	var tokenScopes []string
	switch s := scopeClaim.(type) {
	case string:
		tokenScopes = strings.Fields(s)
	case []interface{}:
		for _, scope := range s {
			if str, ok := scope.(string); ok {
				tokenScopes = append(tokenScopes, str)
			}
		}
	case []string:
		tokenScopes = s
	default:
		return fmt.Errorf("unexpected scope claim type: %T", scopeClaim)
	}

	scopeSet := make(map[string]bool)
	for _, s := range tokenScopes {
		scopeSet[s] = true
	}

	for _, required := range a.config.RequiredScopes {
		if !scopeSet[required] {
			return fmt.Errorf("missing required scope: %s", required)
		}
	}

	return nil
}

// getJWKS retrieves the JWKS, using cache when available
func (a *OAuth2Authenticator) getJWKS(ctx context.Context) (jwk.Set, error) {
	a.jwksMu.RLock()
	if a.jwksCache != nil && time.Since(a.jwksCacheTime) < a.config.JWKSCacheDuration {
		defer a.jwksMu.RUnlock()
		return a.jwksCache, nil
	}
	a.jwksMu.RUnlock()

	// Need to refresh cache
	a.jwksMu.Lock()
	defer a.jwksMu.Unlock()

	// Double-check after acquiring write lock
	if a.jwksCache != nil && time.Since(a.jwksCacheTime) < a.config.JWKSCacheDuration {
		return a.jwksCache, nil
	}

	// Get JWKS URI from discovery
	discovery, err := a.getDiscoveryDocument(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch JWKS
	keySet, err := jwk.Fetch(ctx, discovery.JWKSURI, jwk.WithHTTPClient(a.httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	a.jwksCache = keySet
	a.jwksCacheTime = time.Now()

	return keySet, nil
}

// getDiscoveryDocument retrieves the OIDC discovery document
func (a *OAuth2Authenticator) getDiscoveryDocument(ctx context.Context) (*oidcDiscoveryDocument, error) {
	a.discoveryMu.RLock()
	if a.discoveryCache != nil && time.Since(a.discoveryCacheTime) < a.config.JWKSCacheDuration {
		defer a.discoveryMu.RUnlock()
		return a.discoveryCache, nil
	}
	a.discoveryMu.RUnlock()

	a.discoveryMu.Lock()
	defer a.discoveryMu.Unlock()

	// Double-check after acquiring write lock
	if a.discoveryCache != nil && time.Since(a.discoveryCacheTime) < a.config.JWKSCacheDuration {
		return a.discoveryCache, nil
	}

	// Build discovery URL
	issuerURL := strings.TrimSuffix(a.config.IssuerURL, "/")
	discoveryURL := issuerURL + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDiscoveryFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrDiscoveryFailed, resp.StatusCode)
	}

	var discovery oidcDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to decode discovery document: %w", err)
	}

	a.discoveryCache = &discovery
	a.discoveryCacheTime = time.Now()

	return &discovery, nil
}

// extractBearerToken extracts the bearer token from the Authorization header
func extractBearerToken(headers http.Header) string {
	authHeader := headers.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	// Check for Bearer prefix (case-insensitive)
	if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
		return authHeader[7:]
	}

	return ""
}

// ClearCache clears the JWKS and discovery caches
func (a *OAuth2Authenticator) ClearCache() {
	a.jwksMu.Lock()
	a.jwksCache = nil
	a.jwksMu.Unlock()

	a.discoveryMu.Lock()
	a.discoveryCache = nil
	a.discoveryMu.Unlock()
}

// Ensure OAuth2Authenticator implements auth.AuthProvider
var _ auth.AuthProvider = (*OAuth2Authenticator)(nil)
