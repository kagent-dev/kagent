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
	ID string
}

type Agent struct {
	ID string
}

// Authn
type Principal struct {
	User   User
	Agent  Agent
	Claims map[string]any // Raw JWT claims (nil for non-JWT auth)
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
	// add auth to upstream requests of a session for upstream service account.
	UpstreamAuth(r *http.Request, session Session, upstreamPrincipal Principal) error
}

// AccessMode is what a gRPC method requires of its caller.
//
// It lives here rather than beside the server because a library consumer
// registering its own services has to name these, and the server package is
// internal. AccessPublic is the only value that skips authentication; every
// other value is the verb the authorizer is asked to allow.
type AccessMode string

const (
	AccessPublic AccessMode = "public"
	AccessRead   AccessMode = "read"
	AccessCreate AccessMode = "create"
	AccessUpdate AccessMode = "update"
	AccessDelete AccessMode = "delete"
)

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
			// Skip authentication for the health endpoint used by probes.
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
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
