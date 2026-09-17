package auth

import "context"

// ShareContext holds the context derived from a validated X-Share-Token header.
type ShareContext struct {
	Token     string // the raw share token
	SessionID string // session this token grants access to, when it is a session share
	UserID    string // owner's user ID — used for DB lookups
	ReadOnly  bool   // when true, only read operations are allowed

	// AgentInstanceID is the instance this token grants access to, when it is an
	// AgentInstance share.
	//
	// Exactly one of SessionID and AgentInstanceID is set. They are two different
	// kinds of share over two different resources — a session belongs to the older
	// chat path, an instance is the conversation itself — and a single field would
	// have the A2A gateway matching an id that named a session.
	AgentInstanceID string
}

// IsForAgentInstance reports whether this share grants access to the named instance.
//
// Asked rather than assumed: a session share reaching the A2A gateway must not be
// treated as authority over an instance that happens to share its id space.
func (s *ShareContext) IsForAgentInstance(instanceID string) bool {
	return s != nil && s.AgentInstanceID != "" && s.AgentInstanceID == instanceID
}

type shareContextKeyType struct{}

var shareContextKey = shareContextKeyType{}

// ShareContextFrom returns the ShareContext stored in ctx, if any.
func ShareContextFrom(ctx context.Context) (*ShareContext, bool) {
	v, ok := ctx.Value(shareContextKey).(*ShareContext)
	return v, ok && v != nil
}

// ShareContextTo returns a copy of ctx with sc stored as the share context.
func ShareContextTo(ctx context.Context, sc *ShareContext) context.Context {
	return context.WithValue(ctx, shareContextKey, sc)
}
