package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// transientRetries is how many times a read-only MCP request is sent
	// again after a transient HTTP status.
	transientRetries = 2
	// transientRetryBackoff is the wait before the first retry. It doubles
	// for each later retry.
	transientRetryBackoff = 250 * time.Millisecond
	// maxRetryBodyBytes bounds the request body kept in memory for a retry.
	// Read-only MCP requests are a few hundred bytes.
	maxRetryBodyBytes = 64 << 10
)

// transientRetryRoundTripper sends a read-only MCP request again when the
// server answers 502, 503, or 504.
//
// The ADK MCP toolset lists tools before every model call, and a single
// failed tools/list fails the whole agent run. Hosted MCP servers and the
// gateways in front of them return short-lived 5xx responses, so the toolset
// needs a retry below the MCP session. Requests that can change server state,
// such as tools/call, are sent once: a retry could repeat a side effect.
type transientRetryRoundTripper struct {
	base    http.RoundTripper
	retries int
	backoff time.Duration
}

// isReadOnlyMCPMethod reports whether a JSON-RPC method only reads server
// state, so that sending it twice is safe.
func isReadOnlyMCPMethod(method string) bool {
	switch method {
	case "initialize", "ping", "tools/list", "prompts/list", "resources/list", "resources/templates/list":
		return true
	default:
		return false
	}
}

// isTransientStatus reports whether an HTTP status says the server could not
// handle the request right now.
func isTransientStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// jsonRPCMethod returns the method of a single JSON-RPC message. It returns
// "" for batches, responses, and bodies that are not JSON.
func jsonRPCMethod(body []byte) string {
	var msg struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return ""
	}
	return msg.Method
}

// withBody returns a copy of req that sends body.
func withBody(req *http.Request, body []byte) *http.Request {
	r := req.Clone(req.Context())
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	r.ContentLength = int64(len(body))
	return r
}

func (rt *transientRetryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Body == nil || req.Body == http.NoBody {
		return rt.base.RoundTrip(req)
	}

	head, err := io.ReadAll(io.LimitReader(req.Body, maxRetryBodyBytes+1))
	if err != nil {
		_ = req.Body.Close()
		return nil, fmt.Errorf("failed to read MCP request body: %w", err)
	}
	if len(head) > maxRetryBodyBytes || !isReadOnlyMCPMethod(jsonRPCMethod(head)) {
		// Send once, with the bytes already read put back in front of the rest.
		r := req.Clone(req.Context())
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(head), req.Body), req.Body}
		return rt.base.RoundTrip(r)
	}
	_ = req.Body.Close()

	delay := rt.backoff
	for attempt := 0; ; attempt++ {
		resp, err := rt.base.RoundTrip(withBody(req, head))
		if err != nil || attempt == rt.retries || !isTransientStatus(resp.StatusCode) {
			return resp, err
		}
		// Drain the body so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxRetryBodyBytes))
		_ = resp.Body.Close()

		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
		delay *= 2
	}
}
