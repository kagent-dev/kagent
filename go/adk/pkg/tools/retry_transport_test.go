package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// roundTripFunc adapts a function to the http.RoundTripper interface.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func okResponse() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(nil))}
}

var (
	errDialRefused = &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	errReadReset   = &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
)

func TestRetryTransportRetryDecision(t *testing.T) {
	for _, test := range []struct {
		name      string
		method    string
		body      string
		noGetBody bool
		err       error
		wantCalls int
	}{
		{name: "POST dial refused", method: http.MethodPost, body: "{}", err: errDialRefused, wantCalls: 3},
		{name: "POST DNS failure", method: http.MethodPost, body: "{}", err: &net.DNSError{Err: "no such host", Name: "agent"}, wantCalls: 3},
		{name: "POST EOF after delivery", method: http.MethodPost, body: "{}", err: io.EOF, wantCalls: 1},
		{name: "POST unexpected EOF", method: http.MethodPost, body: "{}", err: io.ErrUnexpectedEOF, wantCalls: 1},
		{name: "POST read reset", method: http.MethodPost, body: "{}", err: errReadReset, wantCalls: 1},
		{name: "POST without GetBody", method: http.MethodPost, body: "{}", noGetBody: true, err: errDialRefused, wantCalls: 1},
		{name: "GET EOF", method: http.MethodGet, err: io.EOF, wantCalls: 3},
		{name: "GET read reset", method: http.MethodGet, err: errReadReset, wantCalls: 3},
		{name: "GET dial refused", method: http.MethodGet, err: errDialRefused, wantCalls: 3},
		{name: "bodyless POST EOF", method: http.MethodPost, err: io.EOF, wantCalls: 1},
		{name: "GET other error", method: http.MethodGet, err: errors.New("boom"), wantCalls: 1},
		{name: "GET context canceled", method: http.MethodGet, err: context.Canceled, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			next := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, test.err
			})
			var body io.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			}
			req, err := http.NewRequestWithContext(t.Context(), test.method, "http://agent.invalid/", body)
			if err != nil {
				t.Fatal(err)
			}
			if test.noGetBody {
				req.GetBody = nil
			}

			_, err = newRetryTransport(next, 3, time.Millisecond).RoundTrip(req)
			if !errors.Is(err, test.err) {
				t.Errorf("error = %v, want %v", err, test.err)
			}
			if calls != test.wantCalls {
				t.Errorf("calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

// trackingBody records whether the transport consumed and closed it.
type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func TestRetryTransportSendsOriginalBodyFirst(t *testing.T) {
	original := &trackingBody{Reader: strings.NewReader("payload")}
	var bodies []io.ReadCloser
	var payloads []string
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		bodies = append(bodies, req.Body)
		data, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		payloads = append(payloads, string(data))
		if len(bodies) == 1 {
			return nil, errDialRefused
		}
		return okResponse(), nil
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://agent.invalid/", original)
	if err != nil {
		t.Fatal(err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("payload")), nil
	}

	if _, err := newRetryTransport(next, 3, time.Millisecond).RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("calls = %d, want 2", len(bodies))
	}
	if bodies[0] != io.ReadCloser(original) {
		t.Errorf("first attempt body = %T, want the original request body", bodies[0])
	}
	if !original.closed {
		t.Error("original body was not closed")
	}
	if bodies[1] == io.ReadCloser(original) {
		t.Error("retry reused the consumed original body")
	}
	if payloads[0] != "payload" || payloads[1] != "payload" {
		t.Errorf("payloads = %q, want the same payload on every attempt", payloads)
	}
}

func TestRetryTransportReturnsLastErrorWhenExhausted(t *testing.T) {
	calls := 0
	next := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 3 {
			return nil, errReadReset
		}
		return nil, io.EOF
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://agent.invalid/", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = newRetryTransport(next, 3, time.Millisecond).RoundTrip(req)
	if !errors.Is(err, errReadReset) {
		t.Errorf("error = %v, want the last transport error %v", err, errReadReset)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRetryTransportStopsWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	next := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, io.EOF
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent.invalid/", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = newRetryTransport(next, 3, time.Hour).RoundTrip(req)
	if !errors.Is(err, io.EOF) {
		t.Errorf("error = %v, want io.EOF", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// TestRetryTransportDoesNotResendDeliveredRequest exercises a real
// connection: the server reads the request and drops the connection without
// responding, which the client sees as EOF. The request was delivered, so it
// must not be sent again.
func TestRetryTransportDoesNotResendDeliveredRequest(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(server.Close)

	client := &http.Client{Transport: newRetryTransport(http.DefaultTransport, 3, time.Millisecond)}
	resp, err := client.Post(server.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0"}`))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected a transport error")
	}
	if got := received.Load(); got != 1 {
		t.Errorf("server received %d requests, want 1", got)
	}
}

// TestRetryTransportResolvesAgentCardAfterDialFailure covers discovery through
// the real agent card resolver, which issues a bodyless GET.
func TestRetryTransportResolvesAgentCardAfterDialFailure(t *testing.T) {
	card := &a2atype.AgentCard{Name: "worker", Description: "test agent", Version: "1"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != a2asrv.WellKnownAgentCardPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	}))
	t.Cleanup(server.Close)

	var dials atomic.Int32
	base := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if dials.Add(1) == 1 {
			return nil, errDialRefused
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	t.Cleanup(base.CloseIdleConnections)
	client := &http.Client{Transport: newRetryTransport(base, 3, time.Millisecond)}

	resolved, err := agentcard.NewResolver(client).Resolve(t.Context(), server.URL)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Name != card.Name {
		t.Errorf("card name = %q, want %q", resolved.Name, card.Name)
	}
	if got := dials.Load(); got != 2 {
		t.Errorf("dials = %d, want 2", got)
	}
}

func TestNewRetryTransportNormalizesConfig(t *testing.T) {
	for _, test := range []struct {
		name         string
		maxAttempts  int
		baseDelay    time.Duration
		wantAttempts int
		wantDelay    time.Duration
	}{
		{name: "valid", maxAttempts: 5, baseDelay: 10 * time.Millisecond, wantAttempts: 5, wantDelay: 10 * time.Millisecond},
		{name: "zero attempts", maxAttempts: 0, baseDelay: time.Second, wantAttempts: 1, wantDelay: time.Second},
		{name: "negative attempts", maxAttempts: -5, baseDelay: time.Second, wantAttempts: 1, wantDelay: time.Second},
		{name: "zero delay", maxAttempts: 3, wantAttempts: 3, wantDelay: defaultRetryBaseDelay},
		{name: "negative delay", maxAttempts: 3, baseDelay: -time.Second, wantAttempts: 3, wantDelay: defaultRetryBaseDelay},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := newRetryTransport(http.DefaultTransport, test.maxAttempts, test.baseDelay)
			if rt.backoff.Steps != test.wantAttempts || rt.backoff.Duration != test.wantDelay || rt.backoff.Factor != 2 {
				t.Errorf("backoff = %+v, want Steps=%d Duration=%v Factor=2", rt.backoff, test.wantAttempts, test.wantDelay)
			}
		})
	}
}

func TestWithRetryTransport(t *testing.T) {
	custom := roundTripFunc(func(*http.Request) (*http.Response, error) { return okResponse(), nil })
	for _, test := range []struct {
		name      string
		env       map[string]string
		transport http.RoundTripper
		wantWrap  bool
		wantSteps int
		wantDelay time.Duration
	}{
		{name: "disabled by default"},
		{name: "explicitly disabled", env: map[string]string{"KAGENT_A2A_RETRY_ENABLED": "false"}},
		{
			name:      "default transport",
			env:       map[string]string{"KAGENT_A2A_RETRY_ENABLED": "true"},
			wantWrap:  true,
			wantSteps: defaultRetryMaxAttempts,
			wantDelay: defaultRetryBaseDelay,
		},
		{
			name: "configured",
			env: map[string]string{
				"KAGENT_A2A_RETRY_ENABLED":      "true",
				"KAGENT_A2A_RETRY_MAX_ATTEMPTS": "5",
				"KAGENT_A2A_RETRY_BASE_DELAY":   "10ms",
			},
			transport: custom,
			wantWrap:  true,
			wantSteps: 5,
			wantDelay: 10 * time.Millisecond,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for k, v := range test.env {
				t.Setenv(k, v)
			}
			c := &http.Client{Transport: test.transport, Timeout: 5 * time.Second}
			wrapped := withRetryTransport(c)
			if !test.wantWrap {
				if wrapped != c {
					t.Fatal("client changed while retry is disabled")
				}
				return
			}
			if wrapped == c || wrapped.Timeout != c.Timeout {
				t.Fatal("want a shallow copy preserving other client fields")
			}
			rt, ok := wrapped.Transport.(*retryTransport)
			if !ok {
				t.Fatalf("transport = %T, want *retryTransport", wrapped.Transport)
			}
			wantNext := test.transport
			if wantNext == nil {
				wantNext = http.DefaultTransport
			}
			if _, isFunc := wantNext.(roundTripFunc); isFunc {
				if _, ok := rt.next.(roundTripFunc); !ok {
					t.Errorf("next = %T, want the client's transport", rt.next)
				}
			} else if rt.next != wantNext {
				t.Errorf("next = %T, want http.DefaultTransport", rt.next)
			}
			if rt.backoff.Steps != test.wantSteps || rt.backoff.Duration != test.wantDelay {
				t.Errorf("backoff = %+v, want Steps=%d Duration=%v", rt.backoff, test.wantSteps, test.wantDelay)
			}
		})
	}
}
