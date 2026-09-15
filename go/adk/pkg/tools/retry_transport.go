package tools

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// defaultRetryMaxAttempts and defaultRetryBaseDelay are the fallback values
// for env.KagentA2ARetryMaxAttempts and env.KagentA2ARetryBaseDelay. They
// match the 250ms base backoff already used by this repo's gateway-side
// retry policies (AgentgatewayPolicy.spec.traffic.retry on
// humanDoors/federationDoor/federationHub), so operators see consistent
// retry timing whether a call happens to cross the gateway or not.
const (
	defaultRetryMaxAttempts = 3
	defaultRetryBaseDelay   = 250 * time.Millisecond
)

// retryTransport wraps an http.RoundTripper and retries requests that fail
// with a transport-level error — a connection that is refused, reset, or
// closed mid-request (net.OpError, io.EOF, io.ErrUnexpectedEOF) — before
// any HTTP response is received.
//
// It intentionally never retries based on a received response's status
// code, even a 5xx one. Status-code-driven retry is agentgateway's job for
// traffic that passes through it (see AgentgatewayPolicy.spec.traffic.retry
// on the human doors and federation hub/door). This transport instead
// covers the traffic that never reaches agentgateway at all: same-cluster,
// pod-to-pod A2A calls (see argocd/base/applications/kagent/AGENTS.md in
// the iops-gitops repo — "Inside a cluster, A2A is pod-to-pod on port 8080
// and never passes the agentgateway"). Those calls have no gateway-side
// retry to fall back on, and kagent's own A2A client
// (a2aclient.Client.SendMessage) makes exactly one attempt. When the target
// pod is evicted or replaced mid-request, the caller sees a bare
// "failed to send HTTP request: ... EOF" with no automatic recovery.
//
// Retries only happen when the request body can be safely replayed
// (req.GetBody != nil, which the standard library populates for the
// bytes/strings-backed bodies the A2A JSON-RPC client sends), and are
// bounded by both retryMaxAttempts and the request's own context deadline.
type retryTransport struct {
	next        http.RoundTripper
	maxAttempts int
	baseDelay   time.Duration
}

// newRetryTransport wraps next with a retry-on-transport-error transport.
// maxAttempts is clamped to at least 1 (a value of 1 or less disables
// retrying while still going through this code path) and a non-positive
// baseDelay falls back to defaultRetryBaseDelay.
func newRetryTransport(next http.RoundTripper, maxAttempts int, baseDelay time.Duration) *retryTransport {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = defaultRetryBaseDelay
	}
	return &retryTransport{next: next, maxAttempts: maxAttempts, baseDelay: baseDelay}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.GetBody == nil {
		// No way to safely replay the body on a retry attempt; fall back to
		// the previous single-attempt behaviour rather than risk sending a
		// truncated or empty body.
		return t.next.RoundTrip(req)
	}

	var lastErr error
	delay := t.baseDelay
	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		req.Body = body

		resp, err := t.next.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if attempt == t.maxAttempts || !isRetryableTransportError(err) {
			return nil, err
		}

		slog.Warn("Retrying A2A request after transport error",
			"attempt", attempt,
			"url", req.URL.String(),
			"error", err,
		)

		select {
		case <-req.Context().Done():
			return nil, err
		case <-time.After(delay):
		}
		delay *= 2
	}
	return nil, lastErr
}

// isRetryableTransportError reports whether err represents a failure to
// establish or complete a connection, as opposed to a successfully received
// HTTP response (http.RoundTripper only returns a non-nil error when no
// response came back at all). It matches:
//
//   - io.EOF / io.ErrUnexpectedEOF: the peer closed the connection without
//     sending a response — the exact failure a2a-go's JSON-RPC transport
//     surfaces as "failed to send HTTP request: ... EOF" when a target pod
//     is evicted or replaced mid-request.
//   - *net.OpError: dial, read, or write failures (connection refused,
//     connection reset, no route to host, etc.), covering both "the pod
//     was gone before we connected" and "the pod died while we were
//     talking to it".
//
// Deliberately excludes anything else, including context cancellation and
// deadline errors (the caller's context.Context is the source of truth for
// whether to keep trying — no point burning an attempt once it's already
// done) and, most importantly, any error type that could only arise after
// a response was received (there is no such thing from a RoundTripper, but
// this stays narrow on purpose so it can never accidentally start acting
// like a status-code-based retry).
func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// withRetryTransport returns a shallow copy of the client whose transport
// retries transport-level failures per retryTransport above, or c itself
// unchanged when the feature is disabled.
//
// The feature is opt-in: it only activates when KAGENT_A2A_RETRY_ENABLED is
// set to true, since retrying is a behavioural change to every A2A call an
// agent makes and operators should choose it deliberately rather than
// inherit it silently on upgrade. When enabled, KAGENT_A2A_RETRY_MAX_ATTEMPTS
// and KAGENT_A2A_RETRY_BASE_DELAY tune the attempt count and base backoff,
// defaulting to the values that shipped with the original fix
// (defaultRetryMaxAttempts, defaultRetryBaseDelay).
//
// Composed before withOTelTransport (see NewKAgentRemoteA2ATool) so otel
// wraps the retrying transport rather than the other way around: a single
// logical A2A call — including any retries underneath — still produces one
// span, with retrying an invisible implementation detail rather than N
// separately traced attempts.
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
