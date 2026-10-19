package runtimeauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type userAuthenticator struct {
	auth.AuthProvider
	actorIssuer string
}

var _ auth.AuthProvider = (*userAuthenticator)(nil)

// Users preserves the configured user authentication mode while preventing
// workload tokens from being interpreted as user credentials, including in
// insecure or trusted-proxy mode.
func Users(provider auth.AuthProvider, actorIssuer string) auth.AuthProvider {
	return &userAuthenticator{AuthProvider: provider, actorIssuer: actorIssuer}
}

func (u *userAuthenticator) Authenticate(ctx context.Context, headers http.Header, query url.Values) (auth.Session, error) {
	for _, value := range headers.Values("Authorization") {
		scheme, raw, ok := strings.Cut(value, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			continue
		}
		claims := jwt.MapClaims{}
		// Unverified claims can only reject a request here; they never grant access.
		// Opaque user credentials remain the configured provider's responsibility.
		if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil && len(claims) == 0 {
			continue
		}
		issuer, issuerErr := claims.GetIssuer()
		_, actor := claims["actor.ate.dev"]
		if actor || (issuerErr == nil && issuer == u.actorIssuer) {
			return nil, fmt.Errorf("actor credentials require the runtime endpoint")
		}
	}
	return u.AuthProvider.Authenticate(ctx, headers, query)
}
