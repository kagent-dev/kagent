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

type handler func(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error)

func (h handler) Handle(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	return h(ctx, client, req)
}

func A2ARequestHandler(authProvider auth.AuthProvider, agentNns types.NamespacedName) handler {
	return func(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
		var err error
		var resp *http.Response
		defer func() {
			if err != nil && resp != nil {
				resp.Body.Close()
			}
		}()

		if client == nil {
			return nil, fmt.Errorf("a2aClient.httpRequestHandler: http client is nil")
		}
		upstreamPrincipal := auth.Principal{
			Agent: auth.Agent{
				ID: types.NamespacedName{
					Name:      agentNns.Name,
					Namespace: agentNns.Namespace,
				}.String(),
			},
		}

		if session, ok := auth.AuthSessionFrom(ctx); ok {
			if err := authProvider.UpstreamAuth(req, session, upstreamPrincipal); err != nil {
				return nil, fmt.Errorf("a2aClient.httpRequestHandler: upstream auth failed: %w", err)
			}
		}

		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("a2aClient.httpRequestHandler: http request failed: %w", err)
		}

		return resp, nil
	}
}
