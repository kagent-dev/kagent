package auth

import (
	"context"
	"net/http"
	"net/url"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type SimpleSession struct {
	P          auth.Principal
	authHeader string
}

func (s *SimpleSession) Principal() auth.Principal {
	return s.P
}

type InsecureAuthenticator struct{}

func (a *InsecureAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
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

func (a *InsecureAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	// for insecure, just forward user id in header
	if session == nil || session.Principal().User.ID == "" {
		return nil
	}
	r.Header.Set("X-User-Id", session.Principal().User.ID)

	if simpleSession, ok := session.(*SimpleSession); ok && simpleSession.authHeader != "" {
		r.Header.Set("Authorization", simpleSession.authHeader)
	}

	return nil
}
