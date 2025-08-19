package auth

import (
	"context"
	"net/http"

	"trpc.group/trpc-go/trpc-a2a-go/auth"
)

var (
	sessionKey = &struct{}{}
)

type Verb string

type Resource struct {
	Name string
	Type string
}

const (
	VerbGet    Verb = "get"
	VerbList   Verb = "list"
	VerbCreate Verb = "create"
	VerbUpdate Verb = "update"
	VerbDelete Verb = "delete"
)

func DefaultAuthnMiddleware() func(http.Handler) http.Handler {
	return AuthnMiddleware(UnsecureAuthenticator)
}
func DefaultA2AAuthnProvider() auth.Provider {
	return &A2AUnsecureAuthenticator{}
}

func AuthSessionFrom(ctx context.Context) (*Session, bool) {
	v, ok := ctx.Value(sessionKey).(*Session)
	return v, ok && v != nil
}

type Principal struct {
	User  string
	Agent string
}
type Session struct {
	Principal Principal
}

func AuthnMiddleware(authn func(r *http.Request) *Session) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := authn(r)
			if session != nil {
				r = r.WithContext(context.WithValue(r.Context(), sessionKey, session))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func UnsecureAuthenticator(r *http.Request) *Session {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = r.Header.Get("X-User-Id")
	}
	if userID == "" {
		userID = "admin@kagent.dev"
	}
	agentId := r.Header.Get("X-Agent-Name")
	return &Session{
		Principal: Principal{
			User:  userID,
			Agent: agentId,
		},
	}
}

type A2AUnsecureAuthenticator struct{}

func (a *A2AUnsecureAuthenticator) Authenticate(r *http.Request) (*auth.User, error) {
	session := UnsecureAuthenticator(r)
	if session == nil {
		return nil, nil
	}
	if session.Principal.User == "" {
		return nil, nil
	}
	return &auth.User{
		ID: session.Principal.User,
	}, nil
}
