package tools

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"k8s.io/apimachinery/pkg/util/wait"
)

// Fallback values for env.KagentA2ARetryMaxAttempts and
// env.KagentA2ARetryBaseDelay.
const (
	defaultRetryMaxAttempts = 3
	defaultRetryBaseDelay   = 250 * time.Millisecond
)

// retryTransport retries outbound A2A HTTP requests only when resending them
// cannot execute the same input twice:
//
//   - any request whose connection was never established (see isDialError):
//     the remote agent never received a byte, so a resend is the first
//     delivery;
//   - bodyless idempotent requests, such as agent card discovery, on any
//     transport error.
//
// A JSON-RPC send that fails after the connection was established (EOF,
// connection reset) is ambiguous: the agent may already have acted on it.
// Those failures are returned unchanged and left to the A2A layer, which only
// resends inputs the server reports as KAGENT_SEND_NOT_ACCEPTED and otherwise
// recovers through the task (see sendMessageWithRetry and recoverResumedTask).
//
// Status codes are never retried; an HTTP response means the request was
// delivered.
type retryTransport struct {
	next    http.RoundTripper
	backoff wait.Backoff
}

// newRetryTransport wraps next. maxAttempts below 1 is treated as 1 (no
// retry) and a non-positive baseDelay falls back to defaultRetryBaseDelay.
func newRetryTransport(next http.RoundTripper, maxAttempts int, baseDelay time.Duration) *retryTransport {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = defaultRetryBaseDelay
	}
	return &retryTransport{
		next:    next,
		backoff: wait.Backoff{Duration: baseDelay, Factor: 2, Steps: maxAttempts},
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var (
		resp    *http.Response
		lastErr error
		attempt int
	)
	err := wait.ExponentialBackoffWithContext(req.Context(), t.backoff, func(context.Context) (bool, error) {
		attempt++
		attemptReq := req
		if attempt > 1 {
			// The first attempt owns req.Body; the transport closed it.
			// Retries send a fresh copy.
			var err error
			if attemptReq, err = replayableRequest(req); err != nil {
				lastErr = err
				return false, err
			}
		}
		var err error
		resp, err = t.next.RoundTrip(attemptReq)
		if err == nil {
			return true, nil
		}
		lastErr = err
		if !isSafeToRetry(req, err) {
			return false, err
		}
		if attempt == t.backoff.Steps {
			return false, nil
		}
		logging.FromContext(req.Context()).WarnContext(req.Context(), "retrying A2A request after transport error",
			"attempt", attempt,
			"method", req.Method,
			"url", req.URL.String(),
			"error", err,
		)
		return false, nil
	})
	if err == nil {
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, err
}

// replayableRequest returns a copy of req with a fresh body for a retry.
func replayableRequest(req *http.Request) (*http.Request, error) {
	retry := req.Clone(req.Context())
	if isBodyless(req) {
		retry.Body = req.Body
		return retry, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	retry.Body = body
	return retry, nil
}

// isSafeToRetry reports whether resending req after err cannot deliver the
// same input to the remote agent twice.
func isSafeToRetry(req *http.Request, err error) bool {
	if req.Context().Err() != nil {
		return false
	}
	if !isBodyless(req) && req.GetBody == nil {
		return false
	}
	if isDialError(err) {
		return true
	}
	return isBodyless(req) && isIdempotentMethod(req.Method) && isTransportError(err)
}

// isDialError reports whether err happened while establishing the
// connection, before any part of the request was written.
func isDialError(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// isTransportError reports whether err is a connection failure rather than,
// for example, a context cancellation.
func isTransportError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func isBodyless(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody
}

func isIdempotentMethod(method string) bool {
	switch method {
	case "", http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// withRetryTransport returns a shallow copy of c whose transport applies
// retryTransport, or c unchanged when KAGENT_A2A_RETRY_ENABLED is false.
//
// Composed before withOTelTransport so one logical A2A call produces one
// span, whatever retries happen underneath.
func withRetryTransport(c *http.Client) *http.Client {
	if !env.KagentA2ARetryEnabled.Get() {
		return c
	}
	cp := *c
	transport := cp.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	cp.Transport = newRetryTransport(
		transport,
		env.KagentA2ARetryMaxAttempts.Get(),
		env.KagentA2ARetryBaseDelay.Get(),
	)
	return &cp
}
