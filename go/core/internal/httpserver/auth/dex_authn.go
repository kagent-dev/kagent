package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// DexAuthenticator validates Dex-issued JWTs. It fetches the issuer's JWKS
// via OIDC discovery and keeps the key set fresh using an auto-refresh cache.
type DexAuthenticator struct {
	issuer      string
	clientID    string
	userIDClaim string
	cache       *jwk.Cache
	jwksURI     string
}

// DexAuthenticatorConfig holds the parameters for NewDexAuthenticator.
type DexAuthenticatorConfig struct {
	// Issuer is the Dex OIDC issuer URL (e.g. https://dex.example.com).
	Issuer string
	// ClientID is the Dex client ID whose tokens are accepted (used as audience).
	ClientID string
	// UserIDClaim is the JWT claim used as the user's identity (default: "email").
	UserIDClaim string
	// RefreshInterval controls how often the JWKS is re-fetched (default: 15 minutes).
	RefreshInterval time.Duration
}

// NewDexAuthenticator creates a DexAuthenticator and starts the JWKS cache.
// ctx must remain live for the cache refresh goroutine to run.
func NewDexAuthenticator(ctx context.Context, cfg DexAuthenticatorConfig) (*DexAuthenticator, error) {
	if cfg.UserIDClaim == "" {
		cfg.UserIDClaim = "email"
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 15 * time.Minute
	}

	jwksURI, err := discoverJWKSURI(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("dex OIDC discovery for %s: %w", cfg.Issuer, err)
	}

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURI, jwk.WithRefreshInterval(cfg.RefreshInterval)); err != nil {
		return nil, fmt.Errorf("registering JWKS URI %s: %w", jwksURI, err)
	}
	// Warm up the cache so the first request does not pay the round-trip.
	if _, err := cache.Refresh(ctx, jwksURI); err != nil {
		return nil, fmt.Errorf("initial JWKS fetch from %s: %w", jwksURI, err)
	}

	return &DexAuthenticator{
		issuer:      cfg.Issuer,
		clientID:    cfg.ClientID,
		userIDClaim: cfg.UserIDClaim,
		cache:       cache,
		jwksURI:     jwksURI,
	}, nil
}

// Authenticate validates the Bearer JWT in the Authorization header.
// It rejects requests with no token, an invalid token, or a token that fails
// signature/issuer/audience/expiry validation.
func (a *DexAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, _ url.Values) (auth.Session, error) {
	authHeader := reqHeaders.Get("Authorization")
	tokenString, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || tokenString == "" {
		return nil, ErrUnauthenticated
	}

	keySet, err := a.cache.Get(ctx, a.jwksURI)
	if err != nil {
		return nil, fmt.Errorf("dex auth: failed to retrieve JWKS: %w", err)
	}

	parseOpts := []jwt.ParseOption{
		jwt.WithKeySet(keySet, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithIssuer(a.issuer),
		jwt.WithAcceptableSkew(30 * time.Second),
	}
	if a.clientID != "" {
		parseOpts = append(parseOpts, jwt.WithAudience(a.clientID))
	}

	token, err := jwt.ParseString(tokenString, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnauthenticated, err)
	}

	userID := claimString(token, a.userIDClaim)
	if userID == "" && a.userIDClaim != "sub" {
		userID = token.Subject()
	}
	if userID == "" {
		return nil, fmt.Errorf("%w: user identity claim %q is empty", ErrUnauthenticated, a.userIDClaim)
	}

	rawClaims, err := token.AsMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("dex auth: failed to read token claims: %w", err)
	}

	return &SimpleSession{
		P: auth.Principal{
			User:   auth.User{ID: userID},
			Claims: rawClaims,
		},
		authHeader: authHeader,
	}, nil
}

// UpstreamAuth forwards the verified Authorization header and the resolved
// user identity to the downstream agent pod.
func (a *DexAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, _ auth.Principal) error {
	simpleSession, ok := session.(*SimpleSession)
	if !ok {
		return nil
	}
	if simpleSession.authHeader != "" {
		r.Header.Set("Authorization", simpleSession.authHeader)
	}
	if userID := simpleSession.P.User.ID; userID != "" {
		r.Header.Set("X-User-Id", userID)
	}
	return nil
}

// discoverJWKSURI fetches the OIDC discovery document at
// {issuer}/.well-known/openid-configuration and returns the jwks_uri.
func discoverJWKSURI(ctx context.Context, issuer string) (string, error) {
	discoveryURL := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery endpoint returned %d", resp.StatusCode)
	}

	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discovery document missing jwks_uri")
	}
	return doc.JWKSURI, nil
}

// claimString retrieves a string claim from the token by name.
func claimString(token jwt.Token, claim string) string {
	v, ok := token.Get(claim)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
