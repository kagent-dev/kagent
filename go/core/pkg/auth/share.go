package auth

import "context"

// ShareContext grants read-only access from a validated X-Share-Token header.
type ShareContext struct {
	Token  string // the raw share token
	UserID string // owner's user ID — used for DB lookups

	// SessionID is the conversation this token grants access to.
	SessionID string
}

// IsForSession reports whether this share grants access to the named session.
func (s *ShareContext) IsForSession(sessionID string) bool {
	return s != nil && s.SessionID != "" && s.SessionID == sessionID
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
