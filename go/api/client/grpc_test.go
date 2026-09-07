package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type callObservation struct {
	userID      string
	hasDeadline bool
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func TestParseGRPCURL(t *testing.T) {
	tests := []struct {
		url    string
		target string
		secure bool
		valid  bool
	}{
		{url: "http://localhost:8083", target: "passthrough:///localhost:8083", valid: true},
		{url: "https://api.example.com", target: "passthrough:///api.example.com", secure: true, valid: true},
		{url: "localhost:8083"},
		{url: "http://localhost:8083/path"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			target, secure, err := parseGRPCURL(tt.url)
			if !tt.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.target, target)
			assert.Equal(t, tt.secure, secure)
		})
	}
}

func TestWithGRPCTLSOnlyConfiguresHTTPSEndpoints(t *testing.T) {
	api, err := newBaseClient("http://api.example.com", WithGRPCTLS(GRPCTLSConfig{ServerName: "api.internal"}))
	require.NoError(t, err)
	gateway, err := newBaseClient("https://gateway.example.com", WithGRPCTLS(GRPCTLSConfig{ServerName: "gateway.internal"}))
	require.NoError(t, err)

	assert.Nil(t, api.transport.tlsConfig)
	require.NotNil(t, gateway.transport.tlsConfig)
	assert.Equal(t, "gateway.internal", gateway.transport.tlsConfig.ServerName)
}

func TestNewRejectsInvalidURL(t *testing.T) {
	_, err := NewAPI("localhost:8083")
	require.Error(t, err)
	_, err = NewGateway("localhost:8083")
	require.Error(t, err)
}
