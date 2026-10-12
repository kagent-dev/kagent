package models

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// scriptedTransport answers each request with the next of its responses (the
// last one repeats) and records every request body it was sent.
type scriptedTransport struct {
	responses []func() *http.Response
	bodies    []string
}

func (s *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		s.bodies = append(s.bodies, string(body))
	} else {
		s.bodies = append(s.bodies, "")
	}
	i := min(len(s.bodies), len(s.responses)) - 1
	resp := s.responses[i]()
	resp.Request = req
	return resp, nil
}

func tooManyRequests(header http.Header) func() *http.Response {
	return func() *http.Response {
		h := http.Header{"Content-Type": {"application/json"}}
		maps.Copy(h, header)
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Status:     "429 Too Many Requests",
			Header:     h,
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"rate_limit_error","message":"slow down"}}`)),
		}
	}
}

func ok() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}
}

// recordedRateLimitTransport is the transport under test with its waits
// recorded instead of slept.
func recordedRateLimitTransport(base http.RoundTripper, now time.Time) (*rateLimitTransport, *[]time.Duration) {
	var waits []time.Duration
	t := newRateLimitTransport(base)
	t.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	t.now = func() time.Time { return now }
	return t, &waits
}

func postRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://provider.example/v1/messages", strings.NewReader(body))
	require.NoError(t, err)
	return req
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return string(body)
}

func TestRateLimitTransport(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name      string
		responses []func() *http.Response
		wantCalls int
		wantWaits []time.Duration
		// wantMessage is the outcome the caller reads; empty when the call succeeds.
		wantMessage string
	}{
		{
			name:      "a short Retry-After is waited and the call is sent again",
			responses: []func() *http.Response{tooManyRequests(http.Header{"Retry-After": {"2"}}), ok},
			wantCalls: 2,
			wantWaits: []time.Duration{2 * time.Second},
		},
		{
			name:        "without Retry-After the call backs off and gives up after the retry bound",
			responses:   []func() *http.Response{tooManyRequests(nil)},
			wantCalls:   rateLimitMaxRetries + 1,
			wantWaits:   []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
			wantMessage: "model provider rate-limited the request (429), retried 3 times, no Retry-After",
		},
		{
			name:        "a Retry-After over the wait limit fails the call at once",
			responses:   []func() *http.Response{tooManyRequests(http.Header{"Retry-After": {"3600"}})},
			wantCalls:   1,
			wantMessage: "model provider rate-limited the request (429), retried 0 times, Retry-After 1h0m0s, over the 1m0s wait limit",
		},
		{
			name:        "the waits of one call stay within the wait limit",
			responses:   []func() *http.Response{tooManyRequests(http.Header{"Retry-After": {"25"}})},
			wantCalls:   3,
			wantWaits:   []time.Duration{25 * time.Second, 25 * time.Second},
			wantMessage: "model provider rate-limited the request (429), retried 2 times, Retry-After 25s, over the 1m0s wait limit",
		},
		{
			name:      "Retry-After-Ms is preferred over Retry-After",
			responses: []func() *http.Response{tooManyRequests(http.Header{"Retry-After-Ms": {"1500"}, "Retry-After": {"2"}}), ok},
			wantCalls: 2,
			wantWaits: []time.Duration{1500 * time.Millisecond},
		},
		{
			name:      "a Retry-After date is waited until",
			responses: []func() *http.Response{tooManyRequests(http.Header{"Retry-After": {now.Add(10 * time.Second).Format(http.TimeFormat)}}), ok},
			wantCalls: 2,
			wantWaits: []time.Duration{10 * time.Second},
		},
		{
			name:      "any other status is returned as it is",
			responses: []func() *http.Response{ok},
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &scriptedTransport{responses: tt.responses}
			transport, waits := recordedRateLimitTransport(base, now)

			resp, err := transport.RoundTrip(postRequest(t, "the-request"))
			require.NoError(t, err)

			assert.Len(t, base.bodies, tt.wantCalls)
			for i, body := range base.bodies {
				assert.Equal(t, "the-request", body, "request %d carries the body again", i)
			}
			assert.Equal(t, tt.wantWaits, nilIfEmpty(*waits))
			body := readBody(t, resp)
			if tt.wantMessage == "" {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				return
			}
			assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
			assert.Equal(t, "false", resp.Header.Get("X-Should-Retry"), "the SDK must not retry what the transport gave up on")
			var outcome rateLimitBody
			require.NoError(t, json.Unmarshal([]byte(body), &outcome))
			assert.Equal(t, outcome.Message, outcome.Error.Message)
			assert.True(t, strings.HasPrefix(outcome.Message, tt.wantMessage+": "), "message %q starts with %q", outcome.Message, tt.wantMessage)
			assert.Contains(t, outcome.Message, "slow down", "the provider's own answer is kept")
		})
	}
}

func nilIfEmpty(d []time.Duration) []time.Duration {
	if len(d) == 0 {
		return nil
	}
	return d
}

func TestRateLimitTransportStopsWaitingWhenTheTurnEnds(t *testing.T) {
	base := &scriptedTransport{responses: []func() *http.Response{tooManyRequests(http.Header{"Retry-After": {"30"}})}}
	transport := newRateLimitTransport(base)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := postRequest(t, "x").WithContext(ctx)

	_, err := transport.RoundTrip(req)

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, base.bodies, 1)
}

// TestRateLimitedModelCall drives each SDK through BuildHTTPClient against a
// provider that answers every call 429: the call is sent once plus the retry
// bound, never again by the SDK, and the model's error names the rate limit.
func TestRateLimitedModelCall(t *testing.T) {
	const want = "model provider rate-limited the request (429), retried 3 times, Retry-After 0s"
	newProvider := func(t *testing.T) (*httptest.Server, *atomic.Int32) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "0")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"type":"rate_limit_error","message":"slow down"}}`)
		}))
		t.Cleanup(server.Close)
		return server, &calls
	}
	request := func() *model.LLMRequest {
		return &model.LLMRequest{
			Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
			Config:   &genai.GenerateContentConfig{},
		}
	}
	errorOf := func(t *testing.T, llm model.LLM, stream bool) string {
		t.Helper()
		var message string
		for resp, err := range llm.GenerateContent(t.Context(), request(), stream) {
			if err != nil {
				message = err.Error()
				continue
			}
			if resp.ErrorMessage != "" {
				message = resp.ErrorMessage
			}
		}
		return message
	}

	for _, stream := range []bool{true, false} {
		t.Run("anthropic", func(t *testing.T) {
			server, calls := newProvider(t)
			m, err := newAnthropicModelFromConfig(t.Context(), &AnthropicConfig{Model: "claude-test", BaseUrl: server.URL}, "test-key")
			require.NoError(t, err)

			assert.Contains(t, errorOf(t, m, stream), want)
			assert.EqualValues(t, rateLimitMaxRetries+1, calls.Load())
		})
		t.Run("openai", func(t *testing.T) {
			server, calls := newProvider(t)
			t.Setenv("OPENAI_API_KEY", "test-key")
			m, err := NewOpenAIModel(t.Context(), &OpenAIConfig{Model: "gpt-test", BaseUrl: server.URL})
			require.NoError(t, err)

			assert.Contains(t, errorOf(t, m, stream), want)
			assert.EqualValues(t, rateLimitMaxRetries+1, calls.Load())
		})
		t.Run("ollama", func(t *testing.T) {
			server, calls := newProvider(t)
			m, err := NewOllamaModel(t.Context(), &OllamaConfig{Model: "llama-test", Host: server.URL})
			require.NoError(t, err)

			assert.Contains(t, errorOf(t, m, stream), want)
			assert.EqualValues(t, rateLimitMaxRetries+1, calls.Load())
		})
		t.Run("gemini", func(t *testing.T) {
			server, calls := newProvider(t)
			httpClient, err := BuildHTTPClient(TransportConfig{})
			require.NoError(t, err)
			m, err := gemini.NewModel(t.Context(), "gemini-test", &genai.ClientConfig{
				APIKey:      "test-key",
				HTTPClient:  httpClient,
				HTTPOptions: genai.HTTPOptions{BaseURL: server.URL},
			})
			require.NoError(t, err)

			assert.Contains(t, errorOf(t, m, stream), want)
			assert.EqualValues(t, rateLimitMaxRetries+1, calls.Load())
		})
	}
}
