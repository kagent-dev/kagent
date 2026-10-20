package runtimeauth

import (
	"context"
	"errors"
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

// ErrActorCredentials directs transports to the Actor JWT authenticator.
// It never establishes an identity or grants access on its own.
var ErrActorCredentials = errors.New("actor credentials require Actor JWT verification")

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
		// Unverified claims only exclude user authentication; actor identity must
		// be verified separately.
		// Opaque user credentials remain the configured provider's responsibility.
		if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil && len(claims) == 0 {
			continue
		}
		issuer, issuerErr := claims.GetIssuer()
		_, actor := claims["actor.ate.dev"]
		if actor || (issuerErr == nil && issuer == u.actorIssuer) {
			return nil, ErrActorCredentials
		}
	}
	return u.AuthProvider.Authenticate(ctx, headers, query)
}
