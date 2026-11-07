package checkpoint

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type retryBackoff struct {
	delay time.Duration
	cap   time.Duration
}

// Keep jitter at the cap too: synchronized callers must not converge onto the
// same interval after a long wait. Every delay is within 90-100% of its bound.
func (b *retryBackoff) next() time.Duration {
	delay := b.delay
	b.delay = min(2*b.delay, b.cap)
	return delay - time.Duration(rand.Int64N(int64(delay/10)+1))
}

func (s *Service) waitRetry(ctx context.Context, delay time.Duration) error {
	timer := s.clock.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C():
		return nil
	}
}

func permanentOperationError(err error) *status.Status {
	var failure *status.Status
	message := "Checkpoint recovery cannot continue safely"
	var public *serviceerrors.Error
	if errors.As(err, &public) {
		message = public.Message()
	}
	switch serviceerrors.CodeOf(err) {
	case serviceerrors.CodeFailedPrecondition:
		failure = status.New(codes.FailedPrecondition, message)
	case serviceerrors.CodeInvalidArgument:
		failure = status.New(codes.InvalidArgument, message)
	case serviceerrors.CodePermissionDenied:
		failure = status.New(codes.PermissionDenied, message)
	case serviceerrors.CodeUnauthenticated:
		failure = status.New(codes.Unauthenticated, message)
	case "":
		failure = status.Convert(err)
	default:
		return nil
	}
	switch failure.Code() {
	case codes.FailedPrecondition, codes.InvalidArgument, codes.PermissionDenied, codes.Unauthenticated:
		// Backend status text and details can quote private configuration. Only
		// service-owned public messages cross this durable API error boundary.
		return status.New(failure.Code(), message)
	default:
		return nil
	}
}
