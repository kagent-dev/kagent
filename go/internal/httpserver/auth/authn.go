package auth

import (
	"context"
	"net/http"
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
		return nil
	}
	agentId := r.Header.Get("X-Agent-Name")
	return &Session{
		Principal: Principal{
			User:  userID,
			Agent: agentId,
		},
	}
}
