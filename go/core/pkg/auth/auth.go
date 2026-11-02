package auth

import (
	"context"
	"net/http"
	"net/url"

	"github.com/kagent-dev/kagent/go/api/authorization"
)

type Verb string

const (
	VerbList   Verb = "list"
	VerbGet    Verb = "get"
	VerbCreate Verb = "create"
	VerbUpdate Verb = "update"
	VerbDelete Verb = "delete"
)

type Resource struct {
	Type      string
	Namespace string
	Name      string
}

// RuntimeBinding identifies a verified runtime caller and its immutable Session binding.
// Caller-supplied agent headers never establish an Actor identity.
type RuntimeBinding struct {
	SessionID string
	// CreatorID selects the Session's data partition, never the caller's authority.
	CreatorID string
}

// Principal describes the authenticated caller, independently of the request's
// destination. RuntimeBinding is present only for a verified runtime caller.
type Principal struct {
	// ID is an opaque, provider-normalized identity, independent of credentials.
	ID             string
	RuntimeBinding *RuntimeBinding
	Claims         map[string]any // Authenticated provider attributes, such as JWT claims.
}

type Session interface {
	Principal() Principal
}

// ControlPlaneSession identifies an internal controller call without human
// credentials. It does not assert a Substrate Actor identity. AuthProvider and
// Authorizer implementations may explicitly allow, deny, or authenticate these
// calls; actor credentials for runtime tool calls remain a separate concern.
type ControlPlaneSession struct{}

var _ Session = ControlPlaneSession{}

func (ControlPlaneSession) Principal() Principal { return Principal{} }

// Responsibilities:
// - Authenticate:
//   - a2a requests from ui/cli (human users)
//   - api requests from users/agents
//
// - Forward auth credentials to upstream agents
type AuthProvider interface {
	Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (Session, error)
	// UpstreamAuth authenticates an authorized upstream request as this caller.
	// destinationSessionID identifies the target conversation; it is empty for
	// upstreams without a Session, such as sandbox guest APIs.
	// For A2A calls, UpstreamMethodFrom(r.Context()) names the outgoing method.
	UpstreamAuth(r *http.Request, session Session, destinationSessionID string) error
}

// AccessMode is what a gRPC method requires of its caller.
//
// It lives here rather than beside the server because a library consumer
// registering its own services has to name these, and the server package is
// internal. AccessPublic permits anonymous calls. AccessRuntime requires a
// verified actor; other modes accept either identity and authorize in services.
type AccessMode string

const (
	AccessPublic  AccessMode = "public"
	AccessRuntime AccessMode = "runtime"
	AccessRead    AccessMode = "read"
	AccessCreate  AccessMode = "create"
	AccessUpdate  AccessMode = "update"
	AccessDelete  AccessMode = "delete"
)

// Authz
type Authorizer interface {
	Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error
}

type CollectionAuthorizer interface {
	Authorizer
	Scope(ctx context.Context, principal Principal, verb Verb, resourceType string) (authorization.AuthorizationScope, error)
}

// NoopAuthorizer permits every action and collection entry.
type NoopAuthorizer struct{}

var _ CollectionAuthorizer = NoopAuthorizer{}

func (NoopAuthorizer) Check(context.Context, Principal, Verb, Resource) error {
	return nil
}

func (NoopAuthorizer) Scope(context.Context, Principal, Verb, string) (authorization.AuthorizationScope, error) {
	return authorization.AuthorizationScope{Kind: authorization.ScopeAll}, nil
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

type upstreamMethodKeyType struct{}

var upstreamMethodKey = upstreamMethodKeyType{}

// UpstreamMethodFrom returns the outgoing a2a-go method, such as "SendMessage", or false outside an A2A call.
// A retried streaming send that was already accepted goes out as "SubscribeToTask".
func UpstreamMethodFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(upstreamMethodKey).(string)
	return v, ok
}

// UpstreamMethodTo returns a copy of ctx that names the outgoing a2a-go method.
func UpstreamMethodTo(ctx context.Context, method string) context.Context {
	return context.WithValue(ctx, upstreamMethodKey, method)
}

func AuthnMiddleware(authn AuthProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, err := authn.Authenticate(r.Context(), r.Header, r.URL.Query())
			if err != nil || session == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			r = r.WithContext(AuthSessionTo(r.Context(), session))
			next.ServeHTTP(w, r)
		})
	}
}
