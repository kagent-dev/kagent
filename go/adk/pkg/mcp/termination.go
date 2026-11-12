package mcp

import (
	"fmt"
	"net/http"
)

// sessionPreservingTransport suppresses MCP session termination when explicitly
// disabled. The SDK always sends DELETE from Close; acknowledging it locally
// preserves the remote session while letting the SDK cancel its local streams.
type sessionPreservingTransport struct {
	base http.RoundTripper
}

func (s *sessionPreservingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodDelete || req.Header.Get("Mcp-Session-Id") == "" {
		return s.base.RoundTrip(req)
	}
	if req.Body != nil {
		if err := req.Body.Close(); err != nil {
			return nil, fmt.Errorf("failed to close MCP session termination request body: %w", err)
		}
	}
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

var _ http.RoundTripper = (*sessionPreservingTransport)(nil)
