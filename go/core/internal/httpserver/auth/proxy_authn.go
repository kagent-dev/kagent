package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

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
	if len(reqHeaders.Values("Authorization")) != 1 {
		return nil, ErrUnauthenticated
	}
	authHeader := reqHeaders.Get("Authorization")

	tokenString, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return nil, ErrUnauthenticated
	}

	// The configured trusted proxy must verify this credential before forwarding it.
	// Kubernetes credentials are independently verified by ServiceAccountProvider.
	rawClaims, err := parseJWTPayload(tokenString)
	if err != nil {
		return nil, ErrUnauthenticated
	}

	// Direct user call: identity comes from the OIDC JWT claims.
	userID, _ := rawClaims[a.userIDClaim].(string)
	if userID == "" && a.userIDClaim != "sub" {
		userID, _ = rawClaims["sub"].(string)
	}
	if userID == "" {
		return nil, ErrUnauthenticated
	}
	var expiresAt time.Time
	if raw, exists := rawClaims["exp"]; exists {
		seconds, ok := raw.(float64)
		if !ok || seconds <= float64(time.Now().Unix()) {
			return nil, ErrUnauthenticated
		}
		expiresAt = time.Unix(int64(seconds), 0)
	}
	issuer, _ := rawClaims["iss"].(string)
	return &SimpleSession{
		P: auth.Principal{
			ID:     auth.PrincipalID("trusted-proxy:"+issuer, userID),
			Claims: rawClaims,
		},
		authHeader: authHeader,
		expiresAt:  expiresAt,
	}, nil
}

func (a *ProxyAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, destinationSessionID string) error {
	return auth.ForwardCredential(r, session)
}

// parseJWTPayload parses JWT claims without signature verification. Callers must
// establish trust through the configured proxy or an independent verifier.
func parseJWTPayload(tokenString string) (map[string]any, error) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tokenString, claims); err != nil {
		return nil, err
	}
	return claims, nil
}
