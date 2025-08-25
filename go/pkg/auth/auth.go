package auth

import (
	"context"
	"net/http"
)

type Verb string

type Resource struct {
	Name string
	Type string
}

const (
	VerbGet    Verb = "get"
	VerbCreate Verb = "create"
	VerbUpdate Verb = "update"
	VerbDelete Verb = "delete"
)

// Authn
type Principal struct {
	User  string
	Agent string
}
type Session struct {
	Principal Principal
}

// Responsibilities:
// - Authenticate:
//   - a2a requests from ui/cli (human users)
//   - api requests from users/agents
//
// - Forward auth credentials to upstream agents
type AuthProvider interface {
	Authenticate(r *http.Request) (*Session, error)
	// add auth to upstream requests of a session
	UpstreamAuth(r *http.Request, session *Session) error
}

// Authz
type Authorizer interface {
	Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error
}
