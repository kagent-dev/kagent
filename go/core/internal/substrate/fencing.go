package substrate

import (
	"context"
	"strconv"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fencingTokenTag is the protobuf field number of fencing_token on the Pause,
// Suspend and Resume requests.
const fencingTokenTag = 10001

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

// fenced sends a request with the fencing token on ctx. A Substrate whose
// Actor API predates the token refuses every field it has no descriptor for;
// the client then remembers that this ate-api takes no token, says so once and
// sends the request, and every later one, without it. The runtime boundary
// works as before the token, only a frozen holder's late Pause or Suspend is
// no longer refused.
func fenced[T any](ctx context.Context, c *Client, call func(*ateapipb.FencingToken) (T, error)) (T, error) {
	token := FencingTokenFrom(ctx)
	if token == nil || c.fencingUnsupported.Load() {
		return call(nil)
	}
	resp, err := call(token)
	if !refusesFencingToken(err) {
		return resp, err
	}
	if c.fencingUnsupported.CompareAndSwap(false, true) {
		logging.FromContext(ctx).WarnContext(ctx, "fencing token refused by Substrate: Pause, Suspend and Resume are sent unfenced, so a frozen quiescence holder's late Pause or Suspend is not refused until Substrate is upgraded",
			"ate_api", c.cfg.AteAPIEndpoint, "error", err)
	}
	return call(nil)
}

// refusesFencingToken reports whether err is ate-api's refusal of the
// fencing_token field as unknown.
func refusesFencingToken(err error) bool {
	s, ok := status.FromError(err)
	return ok && s.Code() == codes.InvalidArgument &&
		strings.Contains(s.Message(), "unknown field with protobuf tag "+strconv.Itoa(fencingTokenTag))
}
