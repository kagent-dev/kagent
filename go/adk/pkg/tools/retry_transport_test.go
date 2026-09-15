package tools

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roundTripFunc adapts a function to the http.RoundTripper interface, used
// throughout this file to stub the transport's next hop without pulling in
// a mocking library, consistent with the rest of this package.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.invalid/", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to build test request: %v", err)
	}
	return req
}

func okResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
}

func TestRetryTransport_SucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return okResponse(), nil
	})

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	resp, err := rt.RoundTrip(newTestRequest(t, "body"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if calls != 1 {
		t.Errorf("got %d calls, want 1", calls)
	}
}

func TestRetryTransport_RetriesOnEOFThenSucceeds(t *testing.T) {
	calls := 0
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls < 2 {
			return nil, io.EOF
		}
		return okResponse(), nil
	})

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	rt.RoundTrip(newTestRequest(t, "body")) //nolint:errcheck // exercised for call count below

	if calls != 2 {
		t.Errorf("got %d calls, want 2 (one failure, one success)", calls)
	}
}

func TestRetryTransport_RetriesOnNetOpErrorThenSucceeds(t *testing.T) {
	calls := 0
	opErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return nil, opErr
		}
		return okResponse(), nil
	})

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	resp, err := rt.RoundTrip(newTestRequest(t, "body"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a response on eventual success")
	}
	if calls != defaultRetryMaxAttempts {
		t.Errorf("got %d calls, want %d", calls, defaultRetryMaxAttempts)
	}
}

func TestRetryTransport_GivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, io.EOF
	})

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	_, err := rt.RoundTrip(newTestRequest(t, "body"))
	if !errors.Is(err, io.EOF) {
		t.Errorf("got error %v, want io.EOF", err)
	}
	if calls != defaultRetryMaxAttempts {
		t.Errorf("got %d calls, want %d (exhausted retries)", calls, defaultRetryMaxAttempts)
	}
}

func TestRetryTransport_DoesNotRetryNonTransportError(t *testing.T) {
	calls := 0
	wantErr := errors.New("some non-retryable error")
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, wantErr
	})

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	_, err := rt.RoundTrip(newTestRequest(t, "body"))
	if !errors.Is(err, wantErr) {
		t.Errorf("got error %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Errorf("got %d calls, want 1 (no retry for non-transport error)", calls)
	}
}

func TestRetryTransport_DoesNotRetryWhenBodyCannotBeReplayed(t *testing.T) {
	calls := 0
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, io.EOF
	})

	req := newTestRequest(t, "body")
	req.GetBody = nil // simulate a body that can't be safely re-read

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	_, err := rt.RoundTrip(req)
	if !errors.Is(err, io.EOF) {
		t.Errorf("got error %v, want io.EOF", err)
	}
	if calls != 1 {
		t.Errorf("got %d calls, want 1 (body not replayable, no retry)", calls)
	}
}

func TestRetryTransport_ReplaysBodyOnEachAttempt(t *testing.T) {
	const wantBody = "the-request-body"
	calls := 0
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		got, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("failed to read request body on attempt %d: %v", calls, err)
		}
		if string(got) != wantBody {
			t.Errorf("attempt %d: got body %q, want %q", calls, got, wantBody)
		}
		if calls < 2 {
			return nil, io.EOF
		}
		return okResponse(), nil
	})

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	if _, err := rt.RoundTrip(newTestRequest(t, wantBody)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("got %d calls, want 2", calls)
	}
}

func TestRetryTransport_StopsRetryingWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			// Cancel the context right after the first failure so the
			// retry loop's context select fires instead of time.After.
			cancel()
		}
		return nil, io.EOF
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid/", strings.NewReader("body"))
	if err != nil {
		t.Fatalf("failed to build test request: %v", err)
	}

	rt := newRetryTransport(next, defaultRetryMaxAttempts, defaultRetryBaseDelay)
	_, err = rt.RoundTrip(req)
	if !errors.Is(err, io.EOF) {
		t.Errorf("got error %v, want io.EOF", err)
	}
	if calls != 1 {
		t.Errorf("got %d calls, want 1 (context cancelled before second attempt)", calls)
	}
}

func TestRetryTransport_UsesConfiguredMaxAttemptsAndBaseDelay(t *testing.T) {
	calls := 0
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, io.EOF
	})

	rt := newRetryTransport(next, 2, time.Millisecond)
	start := time.Now()
	_, err := rt.RoundTrip(newTestRequest(t, "body"))
	elapsed := time.Since(start)

	if !errors.Is(err, io.EOF) {
		t.Errorf("got error %v, want io.EOF", err)
	}
	if calls != 2 {
		t.Errorf("got %d calls, want 2 (configured max attempts)", calls)
	}
	if elapsed < time.Millisecond {
		t.Errorf("got elapsed %v, want at least the configured base delay", elapsed)
	}
}

func TestNewRetryTransport_ClampsInvalidMaxAttempts(t *testing.T) {
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) { return okResponse(), nil })

	rt := newRetryTransport(next, 0, defaultRetryBaseDelay)
	if rt.maxAttempts != 1 {
		t.Errorf("got maxAttempts %d, want 1 (clamped from 0)", rt.maxAttempts)
	}

	rt = newRetryTransport(next, -5, defaultRetryBaseDelay)
	if rt.maxAttempts != 1 {
		t.Errorf("got maxAttempts %d, want 1 (clamped from -5)", rt.maxAttempts)
	}
}

func TestNewRetryTransport_ClampsInvalidBaseDelay(t *testing.T) {
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) { return okResponse(), nil })

	rt := newRetryTransport(next, defaultRetryMaxAttempts, 0)
	if rt.baseDelay != defaultRetryBaseDelay {
		t.Errorf("got baseDelay %v, want %v (clamped from 0)", rt.baseDelay, defaultRetryBaseDelay)
	}

	rt = newRetryTransport(next, defaultRetryMaxAttempts, -time.Second)
	if rt.baseDelay != defaultRetryBaseDelay {
		t.Errorf("got baseDelay %v, want %v (clamped from negative)", rt.baseDelay, defaultRetryBaseDelay)
	}
}

func TestIsRetryableTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "io.EOF", err: io.EOF, want: true},
		{name: "io.ErrUnexpectedEOF", err: io.ErrUnexpectedEOF, want: true},
		{name: "wrapped io.EOF", err: errWrap(io.EOF), want: true},
		{name: "net.OpError", err: &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, want: true},
		{name: "context.DeadlineExceeded", err: context.DeadlineExceeded, want: false},
		{name: "context.Canceled", err: context.Canceled, want: false},
		{name: "generic error", err: errors.New("boom"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableTransportError(tt.err); got != tt.want {
				t.Errorf("isRetryableTransportError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// errWrap wraps err in a plain fmt.Errorf-style wrapper so tests can verify
// errors.Is-based matching survives standard error wrapping.
func errWrap(err error) error {
	return &wrappedErr{err: err}
}

type wrappedErr struct{ err error }

func (w *wrappedErr) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrappedErr) Unwrap() error { return w.err }

func TestWithRetryTransport_WrapsDefaultTransportWhenNil(t *testing.T) {
	t.Setenv("KAGENT_A2A_RETRY_ENABLED", "true")

	c := &http.Client{Timeout: 5 * time.Second}
	wrapped := withRetryTransport(c)

	if wrapped == c {
		t.Fatal("withRetryTransport must return a copy, not the same *http.Client")
	}
	rt, ok := wrapped.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("got transport type %T, want *retryTransport", wrapped.Transport)
	}
	if rt.next != http.DefaultTransport {
		t.Errorf("expected retryTransport to wrap http.DefaultTransport when client.Transport is nil")
	}
	if wrapped.Timeout != c.Timeout {
		t.Errorf("got Timeout %v, want %v (shallow copy should preserve other fields)", wrapped.Timeout, c.Timeout)
	}
}

func TestWithRetryTransport_WrapsExistingTransport(t *testing.T) {
	t.Setenv("KAGENT_A2A_RETRY_ENABLED", "true")

	custom := roundTripFunc(func(req *http.Request) (*http.Response, error) { return okResponse(), nil })
	c := &http.Client{Transport: custom}
	wrapped := withRetryTransport(c)

	rt, ok := wrapped.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("got transport type %T, want *retryTransport", wrapped.Transport)
	}
	if _, ok := rt.next.(roundTripFunc); !ok {
		t.Errorf("expected retryTransport to wrap the client's existing transport, got %T", rt.next)
	}
}

func TestWithRetryTransport_DisabledByDefault(t *testing.T) {
	// KAGENT_A2A_RETRY_ENABLED intentionally left unset: the feature must be
	// off unless explicitly opted into.
	c := &http.Client{Timeout: 5 * time.Second}
	wrapped := withRetryTransport(c)

	if wrapped != c {
		t.Fatal("withRetryTransport must return the same *http.Client unchanged when the feature is disabled")
	}
	if _, ok := wrapped.Transport.(*retryTransport); ok {
		t.Error("expected no retryTransport to be installed when KAGENT_A2A_RETRY_ENABLED is unset")
	}
}

func TestWithRetryTransport_ExplicitlyDisabled(t *testing.T) {
	t.Setenv("KAGENT_A2A_RETRY_ENABLED", "false")

	c := &http.Client{Timeout: 5 * time.Second}
	wrapped := withRetryTransport(c)

	if wrapped != c {
		t.Fatal("withRetryTransport must return the same *http.Client unchanged when explicitly disabled")
	}
}

func TestWithRetryTransport_UsesConfiguredMaxAttemptsAndBaseDelay(t *testing.T) {
	t.Setenv("KAGENT_A2A_RETRY_ENABLED", "true")
	t.Setenv("KAGENT_A2A_RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("KAGENT_A2A_RETRY_BASE_DELAY", "10ms")

	c := &http.Client{}
	wrapped := withRetryTransport(c)

	rt, ok := wrapped.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("got transport type %T, want *retryTransport", wrapped.Transport)
	}
	if rt.maxAttempts != 5 {
		t.Errorf("got maxAttempts %d, want 5 (from KAGENT_A2A_RETRY_MAX_ATTEMPTS)", rt.maxAttempts)
	}
	if rt.baseDelay != 10*time.Millisecond {
		t.Errorf("got baseDelay %v, want 10ms (from KAGENT_A2A_RETRY_BASE_DELAY)", rt.baseDelay)
	}
}

func TestWithRetryTransport_FallsBackToDefaultsWhenTuningVarsUnset(t *testing.T) {
	t.Setenv("KAGENT_A2A_RETRY_ENABLED", "true")

	c := &http.Client{}
	wrapped := withRetryTransport(c)

	rt, ok := wrapped.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("got transport type %T, want *retryTransport", wrapped.Transport)
	}
	if rt.maxAttempts != defaultRetryMaxAttempts {
		t.Errorf("got maxAttempts %d, want default %d", rt.maxAttempts, defaultRetryMaxAttempts)
	}
	if rt.baseDelay != defaultRetryBaseDelay {
		t.Errorf("got baseDelay %v, want default %v", rt.baseDelay, defaultRetryBaseDelay)
	}
}
