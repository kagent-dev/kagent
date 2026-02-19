package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

var ErrUnauthenticated = errors.New("unauthenticated: missing or invalid Authorization header")

// ClaimsConfig holds configurable JWT claim names
type ClaimsConfig struct {
	UserID string // Default: "sub"
	Email  string // Default: "email"
	Name   string // Default: tries "name", "preferred_username"
	Groups string // Default: tries "groups", "cognito:groups", "roles"
}

type ProxyAuthenticator struct {
	claims ClaimsConfig
}

func NewProxyAuthenticator(claims ClaimsConfig) *ProxyAuthenticator {
	return &ProxyAuthenticator{claims: claims}
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

		userID := a.getStringClaim(rawClaims, a.claims.UserID, "sub")
		if userID == "" {
			return nil, ErrUnauthenticated
		}

		return &SimpleSession{
			P: auth.Principal{
				User: auth.User{
					ID:    userID,
					Email: a.getStringClaim(rawClaims, a.claims.Email, "email"),
					Name:  a.getStringClaim(rawClaims, a.claims.Name, "name", "preferred_username"),
				},
				Groups: a.getGroupsClaim(rawClaims),
				Agent: auth.Agent{
					ID: agentID, // Include agent identity if present
				},
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

// getStringClaim tries configured key first, then fallbacks
func (a *ProxyAuthenticator) getStringClaim(claims map[string]any, configured string, fallbacks ...string) string {
	if configured != "" {
		if v, ok := claims[configured].(string); ok {
			return v
		}
	}
	for _, key := range fallbacks {
		if v, ok := claims[key].(string); ok {
			return v
		}
	}
	return ""
}

// getGroupsClaim tries configured key first, then common provider claim names
func (a *ProxyAuthenticator) getGroupsClaim(claims map[string]any) []string {
	fallbacks := []string{"groups", "cognito:groups", "roles"}
	keysToTry := fallbacks
	if a.claims.Groups != "" {
		keysToTry = append([]string{a.claims.Groups}, fallbacks...)
	}

	for _, key := range keysToTry {
		switch v := claims[key].(type) {
		case []any:
			groups := make([]string, 0, len(v))
			for _, g := range v {
				if s, ok := g.(string); ok {
					groups = append(groups, s)
				}
			}
			if len(groups) > 0 {
				return groups
			}
		case string:
			if v != "" {
				groups := strings.Split(v, ",")
				result := make([]string, 0, len(groups))
				for _, g := range groups {
					trimmed := strings.TrimSpace(g)
					if trimmed != "" {
						result = append(result, trimmed)
					}
				}
				return result
			}
		}
	}
	return []string{}
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
