package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/kagent-dev/kagent/go/pkg/auth"
	"k8s.io/apimachinery/pkg/types"
)

type SimpleSession struct {
	P          auth.Principal
	authHeader string
}

func (s *SimpleSession) Principal() auth.Principal {
	return s.P
}

type UnsecureAuthenticator struct{}

func (a *UnsecureAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	userID := query.Get("user_id")
	if userID == "" {
		userID = reqHeaders.Get("X-User-Id")
	}
	if userID == "" {
		userID = "admin@kagent.dev"
	}
	agentId := reqHeaders.Get("X-Agent-Name")
	authHeader := reqHeaders.Get("Authorization")

	return &SimpleSession{
		P: auth.Principal{
			User: auth.User{
				ID: userID,
			},
			Agent: auth.Agent{
				ID: agentId,
			},
		},
		authHeader: authHeader,
	}, nil
}

func (a *UnsecureAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	// for unsecure, just forward user id in header
	if session == nil || session.Principal().User.ID == "" {
		return nil
	}
	r.Header.Set("X-User-Id", session.Principal().User.ID)

	if simpleSession, ok := session.(*SimpleSession); ok && simpleSession.authHeader != "" {
		r.Header.Set("Authorization", simpleSession.authHeader)
	}

	return nil
}

func NewA2AAuthenticator(provider auth.AuthProvider) *A2AAuthenticator {
	return &A2AAuthenticator{
		provider: provider,
	}
}

type A2AAuthenticator struct {
	provider auth.AuthProvider
}

func (p *A2AAuthenticator) Wrap(next http.Handler) http.Handler {
	return auth.AuthnMiddleware(p.provider)(next)
}

// A2AAuthRoundTripper is an http.RoundTripper that injects upstream auth
// headers into outgoing A2A client requests.
type A2AAuthRoundTripper struct {
	Base              http.RoundTripper
	AuthProvider      auth.AuthProvider
	UpstreamPrincipal auth.Principal
}

func (t *A2AAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if session, ok := auth.AuthSessionFrom(req.Context()); ok {
		if err := t.AuthProvider.UpstreamAuth(req, session, t.UpstreamPrincipal); err != nil {
			return nil, fmt.Errorf("a2a auth round tripper: upstream auth failed: %w", err)
		}
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// NewA2AAuthTransport creates an http.RoundTripper that injects upstream auth
// headers for the specified agent.
func NewA2AAuthTransport(authProvider auth.AuthProvider, agentRef types.NamespacedName) http.RoundTripper {
	return &A2AAuthRoundTripper{
		AuthProvider: authProvider,
		UpstreamPrincipal: auth.Principal{
			Agent: auth.Agent{
				ID: agentRef.String(),
			},
		},
	}
}
