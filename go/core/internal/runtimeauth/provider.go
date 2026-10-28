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

type Provider struct {
	users       auth.AuthProvider
	actors      *Authenticator
	store       SessionStore
	actorIssuer string
}

var _ auth.AuthProvider = (*Provider)(nil)

// NewProvider authenticates users and actors on the same HTTP and gRPC APIs.
// Actor credentials never fall back to the configured user provider.
func NewProvider(users auth.AuthProvider, verifier actorJWTVerifier, store SessionStore, actorIssuer string) *Provider {
	return &Provider{users: users, actors: NewAuthenticator(verifier), store: store, actorIssuer: actorIssuer}
}

func (p *Provider) Authenticate(ctx context.Context, headers http.Header, query url.Values) (auth.Session, error) {
	if !actorCredentials(headers, p.actorIssuer) {
		return p.users.Authenticate(ctx, headers, query)
	}
	identity, err := p.actors.Authenticate(ctx, headers, query)
	if err != nil {
		return nil, err
	}
	if headers.Get("X-Share-Token") != "" {
		return nil, fmt.Errorf("actors cannot use share credentials")
	}
	session, err := Session(auth.AuthSessionTo(ctx, identity), p.store)
	if err != nil {
		return nil, err
	}
	if session.Creator == "" || session.GetAgent().GetNamespace() == "" || session.GetAgent().GetName() == "" {
		return nil, fmt.Errorf("actor session has no owner or agent")
	}
	bound := identity.(runtimeSession)
	bound.owner = session.Creator
	return bound, nil
}

func (p *Provider) UpstreamAuth(request *http.Request, session auth.Session, destinationSessionID string) error {
	if session != nil && session.Principal().RuntimeBinding != nil {
		if _, err := Session(auth.AuthSessionTo(request.Context(), session), p.store); err != nil {
			return err
		}
		return p.actors.UpstreamAuth(request, session, destinationSessionID)
	}
	return p.users.UpstreamAuth(request, session, destinationSessionID)
}

func actorCredentials(headers http.Header, actorIssuer string) bool {
	for _, value := range headers.Values("Authorization") {
		scheme, raw, ok := strings.Cut(value, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			continue
		}
		claims := jwt.MapClaims{}
		// Unverified claims only select the verifier; they never grant authority.
		// Opaque user credentials remain the configured provider's responsibility.
		if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil && len(claims) == 0 {
			continue
		}
		issuer, issuerErr := claims.GetIssuer()
		_, actor := claims["actor.ate.dev"]
		if actor || (issuerErr == nil && actorIssuer != "" && issuer == actorIssuer) {
			return true
		}
	}
	return false
}
