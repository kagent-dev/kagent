package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// A model call the provider answers with HTTP 429 is sent again at most
// rateLimitMaxRetries times, and waits at most rateLimitMaxWait in total across
// those retries. A Retry-After that would take the call past the wait limit
// ends it at once: a turn never sits on an hourly rate-limit bucket.
const (
	rateLimitMaxRetries = 3
	rateLimitMaxWait    = time.Minute
	// rateLimitBackoff is the first wait when the provider names none; it
	// doubles with every retry.
	rateLimitBackoff = time.Second
	// rateLimitBodyLimit bounds how much of the provider's own 429 answer the
	// outcome keeps.
	rateLimitBodyLimit = 1024
)

// rateLimitTransport is the one rate-limit policy every model provider's HTTP
// client shares. The provider SDKs differ: the Anthropic SDK sleeps for any
// Retry-After the provider sends, an hour included, while the turn reports
// nothing; others retry briefly or not at all. This transport retries a 429
// within the bounds above, then hands the SDK a final 429 that names the rate
// limit, the retries and the wait, and asks the SDK not to retry it
// (X-Should-Retry: false), so the model call ends with that message and the
// task fails with it.
type rateLimitTransport struct {
	base http.RoundTripper
	// sleep and now are replaced in tests.
	sleep func(context.Context, time.Duration) error
	now   func() time.Time
}

func newRateLimitTransport(base http.RoundTripper) *rateLimitTransport {
	return &rateLimitTransport{base: base, sleep: sleepContext, now: time.Now}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var waited time.Duration
	for retries := 0; ; retries++ {
		resp, err := t.base.RoundTrip(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}

		retryAfter, named := retryAfterOf(resp.Header, t.now())
		wait := retryAfter
		if !named {
			wait = rateLimitBackoff << retries
		}
		switch {
		case waited+wait > rateLimitMaxWait:
			return rateLimitedResponse(resp, rateLimitOutcome(retries, retryAfter, named)+fmt.Sprintf(", over the %s wait limit", rateLimitMaxWait)), nil
		case retries == rateLimitMaxRetries || !replayable(req):
			return rateLimitedResponse(resp, rateLimitOutcome(retries, retryAfter, named)), nil
		}

		logging.FromContext(req.Context()).InfoContext(req.Context(), "model provider rate-limited the request; retrying",
			"status", resp.StatusCode, "retry", retries+1, "max_retries", rateLimitMaxRetries, "wait", wait, "url", req.URL.Redacted())
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, rateLimitBodyLimit))
		_ = resp.Body.Close()
		if err := t.sleep(req.Context(), wait); err != nil {
			return nil, err
		}
		waited += wait

		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req = req.Clone(req.Context())
			req.Body = body
		}
	}
}

// rateLimitOutcome is the message a rate-limited model call ends with.
func rateLimitOutcome(retries int, retryAfter time.Duration, named bool) string {
	wait := "no Retry-After"
	if named {
		wait = "Retry-After " + retryAfter.String()
	}
	return fmt.Sprintf("model provider rate-limited the request (429), retried %d times, %s", retries, wait)
}

// retryAfterOf is the wait the provider asks for, from Retry-After-Ms (sent by
// some providers for sub-second precision) or Retry-After in seconds or as an
// HTTP date; named is false when the response names no usable wait.
func retryAfterOf(h http.Header, now time.Time) (wait time.Duration, named bool) {
	if v := h.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil {
			return max(0, time.Duration(ms*float64(time.Millisecond))), true
		}
	}
	v := h.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil {
		return max(0, time.Duration(s*float64(time.Second))), true
	}
	if at, err := http.ParseTime(v); err == nil {
		return max(0, at.Sub(now).Round(time.Second)), true
	}
	return 0, false
}

// rateLimitedResponse turns the provider's final 429 into the outcome the SDK
// reports. The body is one JSON envelope every provider SDK's error parser
// surfaces the message from (error.message for the Anthropic, OpenAI and
// Gemini SDKs, the whole body for Ollama's, the top-level message for AWS's);
// the message is the outcome followed by the provider's own answer. The header
// X-Should-Retry: false keeps the SDK from retrying it, and dropping the AWS
// error type keeps the AWS SDK from retrying it as a throttle.
func rateLimitedResponse(resp *http.Response, outcome string) *http.Response {
	message := outcome
	// A body the transport did not decode itself is opaque; it is left out.
	if resp.Header.Get("Content-Encoding") == "" {
		provider, _ := io.ReadAll(io.LimitReader(resp.Body, rateLimitBodyLimit))
		if provider = bytes.TrimSpace(provider); len(provider) > 0 {
			message += ": " + string(provider)
		}
	}
	_ = resp.Body.Close()
	body, _ := json.Marshal(rateLimitBody{
		Message: message,
		Error:   rateLimitError{Type: "rate_limit_error", Code: http.StatusTooManyRequests, Message: message},
	})

	out := *resp
	out.Header = resp.Header.Clone()
	out.Header.Del("Content-Encoding")
	out.Header.Del("X-Amzn-ErrorType")
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Content-Length", strconv.Itoa(len(body)))
	out.Header.Set("X-Should-Retry", "false")
	out.ContentLength = int64(len(body))
	out.Body = io.NopCloser(bytes.NewReader(body))
	return &out
}

type rateLimitBody struct {
	Message string         `json:"message"`
	Error   rateLimitError `json:"error"`
}

type rateLimitError struct {
	Type    string `json:"type"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// replayable reports whether req can be sent again.
func replayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
