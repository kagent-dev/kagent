package auth

import (
	"context"
	"net/http"
	"net/url"
)

type Verb string

const (
	VerbGet    Verb = "get"
	VerbCreate Verb = "create"
	VerbUpdate Verb = "update"
	VerbDelete Verb = "delete"
)

type Resource struct {
	Name string
	Type string
}

type User struct {
	ID    string
	Roles []string
}
type Agent struct {
	ID string
}

// Authn
type Principal struct {
	User  User
	Agent Agent
}

type Session interface {
	Principal() Principal
}

// Responsibilities:
// - Authenticate:
//   - a2a requests from ui/cli (human users)
//   - api requests from users/agents
//
// - Forward auth credentials to upstream agents
type AuthProvider interface {
	Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (Session, error)
	// add auth to upstream requests of a session
	UpstreamAuth(r *http.Request, session Session) error
}

// Authz
type Authorizer interface {
	Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error
}

// context utils

type sessionKeyType struct{}

var (
	sessionKey = sessionKeyType{}
)

func AuthSessionFrom(ctx context.Context) (Session, bool) {
	v, ok := ctx.Value(sessionKey).(Session)
	return v, ok && v != nil
}

func AuthSessionTo(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, sessionKey, session)
}

func AuthnMiddleware(authn AuthProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, err := authn.Authenticate(r.Context(), r.Header, r.URL.Query())
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
