package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

type sessionKeyType struct{}

var (
	sessionKey = sessionKeyType{}
)

func AuthSessionFrom(ctx context.Context) (*auth.Session, bool) {
	v, ok := ctx.Value(sessionKey).(*auth.Session)
	return v, ok && v != nil
}

func AuthSessionTo(ctx context.Context, session *auth.Session) context.Context {
	return context.WithValue(ctx, sessionKey, session)
}

func AuthnMiddleware(authn auth.AuthProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, err := authn.Authenticate(r)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if session != nil {
				r = r.WithContext(AuthSessionTo(r.Context(), session))
			}
			next.ServeHTTP(w, r)
		})
	}
}

type UnsecureAuthenticator struct{}

func (a *UnsecureAuthenticator) Authenticate(r *http.Request) (*auth.Session, error) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = r.Header.Get("X-User-Id")
	}
	if userID == "" {
		userID = "admin@kagent.dev"
	}
	agentId := r.Header.Get("X-Agent-Name")
	return &auth.Session{
		Principal: auth.Principal{
			User:  userID,
			Agent: agentId,
		},
	}, nil
}

func (a *UnsecureAuthenticator) UpstreamAuth(r *http.Request, session *auth.Session) error {
	// for unsecure, just forward user id in header
	if session == nil || session.Principal.User == "" {
		return nil
	}
	r.Header.Set("X-User-Id", session.Principal.User)
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
	return AuthnMiddleware(p.provider)(next)
}

type handler func(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error)

func (h handler) Handle(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	return h(ctx, client, req)
}

func A2ARequestHandler(authProvider auth.AuthProvider) handler {
	return func(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
		var err error
		var resp *http.Response
		defer func() {
			if err != nil && resp != nil {
				resp.Body.Close() //nolint:errcheck
			}
		}()

		if client == nil {
			return nil, fmt.Errorf("a2aClient.httpRequestHandler: http client is nil")
		}

		if session, ok := AuthSessionFrom(ctx); ok {
			if err := authProvider.UpstreamAuth(req, session); err != nil {
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
