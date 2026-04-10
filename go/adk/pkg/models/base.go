package models

import (
	"net/http"
	"time"
)

// defaultTimeout is the default execution timeout used by model implementations.
const defaultTimeout = 30 * time.Minute

// TransportConfig holds TLS, passthrough, and header settings shared by all model providers.
type TransportConfig struct {
	Headers               map[string]string
	TLSInsecureSkipVerify *bool
	TLSCACertPath         *string
	TLSDisableSystemCAs   *bool
	APIKeyPassthrough     bool
	Timeout               *int // seconds; nil = defaultTimeout
}

// BuildHTTPClient creates an http.Client with the full transport stack:
// TLS → passthrough auth → custom headers → timeout.
func BuildHTTPClient(tc TransportConfig) (*http.Client, error) {
	transport, err := BuildTLSTransport(
		http.DefaultTransport,
		tc.TLSInsecureSkipVerify,
		tc.TLSCACertPath,
		tc.TLSDisableSystemCAs,
	)
	if err != nil {
		return nil, err
	}

	if tc.APIKeyPassthrough {
		transport = &passthroughAuthTransport{base: transport}
	}

	if len(tc.Headers) > 0 {
		transport = &headerTransport{base: transport, headers: tc.Headers}
	}

	timeout := defaultTimeout
	if tc.Timeout != nil {
		timeout = time.Duration(*tc.Timeout) * time.Second
	}

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

// BearerTokenKey is the context key for storing the bearer token for API key passthrough
var BearerTokenKey = &contextKey{}

type contextKey struct{}

// passthroughAuthTransport wraps an http.RoundTripper and adds the Bearer token from context
type passthroughAuthTransport struct {
	base http.RoundTripper
}

func (t *passthroughAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, ok := req.Context().Value(BearerTokenKey).(string); ok && token != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return t.base.RoundTrip(req)
}

// headerTransport wraps an http.RoundTripper and adds custom headers to all requests
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}
