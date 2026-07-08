package auth

import "context"

// ShareContext holds the context derived from a validated X-Share-Token header.
type ShareContext struct {
	Token     string // the raw share token
	SessionID string // session this token grants access to
	UserID    string // owner's user ID — used for DB lookups
	ReadOnly  bool   // when true, only read operations are allowed
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
