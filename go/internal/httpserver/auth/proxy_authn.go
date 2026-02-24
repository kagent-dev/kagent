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
