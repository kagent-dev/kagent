package substrate

import (
	"context"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

type fencingTokenKey struct{}

// WithFencingToken returns a context whose PauseActor, SuspendActor and
// ResumeActor calls carry token. Substrate records the newest token per Actor
// and refuses a request with an older one (FailedPrecondition), so a holder
// that was taken over cannot change the Actor after its successor settled it.
// A call without a token is admitted as before.
func WithFencingToken(ctx context.Context, token *ateapipb.FencingToken) context.Context {
	return context.WithValue(ctx, fencingTokenKey{}, token)
}

// FencingTokenFrom returns the token WithFencingToken put on ctx, or nil.
func FencingTokenFrom(ctx context.Context) *ateapipb.FencingToken {
	token, _ := ctx.Value(fencingTokenKey{}).(*ateapipb.FencingToken)
	return token
}
