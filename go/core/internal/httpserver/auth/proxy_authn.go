package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

var ErrUnauthenticated = errors.New("unauthenticated: missing or invalid Authorization header")

type ProxyAuthenticator struct {
	userIDClaim string
}

func NewProxyAuthenticator(userIDClaim string) *ProxyAuthenticator {
	if userIDClaim == "" {
		userIDClaim = "sub"
	}
	return &ProxyAuthenticator{userIDClaim: userIDClaim}
}

func (a *ProxyAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	authHeader := reqHeaders.Get("Authorization")

	// Always read agent identity from X-Agent-Name header (used by agents calling back)
	agentID := reqHeaders.Get("X-Agent-Name")

	// If we have a Bearer token, parse JWT
	if tokenString, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		// Parse JWT without validation (oauth2-proxy or k8s service account already validated)
		rawClaims, err := parseJWTPayload(tokenString)
		if err != nil {
			return nil, ErrUnauthenticated
		}

		userID, _ := rawClaims[a.userIDClaim].(string)
		if userID == "" && a.userIDClaim != "sub" {
			userID, _ = rawClaims["sub"].(string)
		}
		if userID == "" {
			return nil, ErrUnauthenticated
		}

		return &SimpleSession{
			P: auth.Principal{
				User:   auth.User{ID: userID},
				Agent:  auth.Agent{ID: agentID},
				Groups: extractGroupsFromClaims(rawClaims),
				Claims: rawClaims,
			},
			authHeader: authHeader,
		}, nil
	}

	// Fall back to service account auth for internal agent-to-controller calls.
	// Requires X-Agent-Name to identify the calling agent.
	if agentID == "" {
		return nil, ErrUnauthenticated
	}

	// Agents authenticate via user_id query param or X-User-Id header
	userID := query.Get("user_id")
	if userID == "" {
		userID = reqHeaders.Get("X-User-Id")
	}
	if userID == "" {
		return nil, ErrUnauthenticated
	}

	return &SimpleSession{
		P: auth.Principal{
			User: auth.User{
				ID: userID,
			},
			Agent: auth.Agent{
				ID: agentID,
			},
		},
		authHeader: authHeader,
	}, nil
}

func (a *ProxyAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	if simpleSession, ok := session.(*SimpleSession); ok && simpleSession.authHeader != "" {
		r.Header.Set("Authorization", simpleSession.authHeader)
	}
	return nil
}

// parseJWTPayload decodes JWT payload without signature verification
func parseJWTPayload(tokenString string) (map[string]any, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// extractGroupsFromClaims pulls the groups claim from JWT claims.
// Handles both []string and []interface{} formats from different OIDC providers.
// Supports nested claims via dot notation (e.g., "realm_access.roles" for Keycloak).
func extractGroupsFromClaims(claims map[string]any) []string {
	// Try common claim names in order of priority
	for _, claimName := range []string{"groups", "cognito:groups", "realm_access.roles"} {
		if groups := extractClaimAsStringSlice(claims, claimName); len(groups) > 0 {
			return groups
		}
	}
	return nil
}

// extractClaimAsStringSlice extracts a claim value as a string slice.
// Supports dot-delimited nested paths (e.g., "realm_access.roles").
func extractClaimAsStringSlice(claims map[string]any, key string) []string {
	raw, ok := resolveClaimValue(claims, key)
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		groups := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				groups = append(groups, s)
			}
		}
		return groups
	}
	return nil
}

// resolveClaimValue resolves a claim from a map, supporting dot-delimited nested paths
// such as "realm_access.roles" in addition to top-level keys like "groups" or "cognito:groups".
func resolveClaimValue(claims map[string]any, key string) (any, bool) {
	// Try direct lookup first (handles keys with colons like "cognito:groups")
	if raw, ok := claims[key]; ok {
		return raw, true
	}
	// Try dot-delimited nested path
	if !strings.Contains(key, ".") {
		return nil, false
	}
	current := any(claims)
	for _, part := range strings.Split(key, ".") {
		nextMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = nextMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
