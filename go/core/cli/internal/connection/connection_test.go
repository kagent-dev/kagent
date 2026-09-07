package connection

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOptionsValidate(t *testing.T) {
	assert.NoError(t, (&Options{UserID: "user@example.com"}).validate())
	assert.Error(t, (&Options{}).validate())
	assert.Error(t, (&Options{UserID: "invalid user"}).validate())
}

func TestOptionsClientsValidateOnlyTheirEndpoint(t *testing.T) {
	options := Options{APIURL: defaultAPIURL, GatewayURL: "invalid", UserID: "user@example.com"}
	api, err := options.APIClient()
	require.NoError(t, err)
	require.NoError(t, api.Close())
	_, err = options.GatewayClient()
	require.Error(t, err)

	options.APIURL, options.GatewayURL = "invalid", defaultGatewayURL
	gateway, err := options.GatewayClient()
	require.NoError(t, err)
	require.NoError(t, gateway.Close())
	_, err = options.APIClient()
	require.Error(t, err)
}

func TestShouldPortForward(t *testing.T) {
	defaultConfig := Options{APIURL: defaultAPIURL, GatewayURL: defaultGatewayURL}
	tests := []struct {
		name     string
		config   Options
		endpoint string
		err      error
		want     bool
	}{
		{name: "default endpoint unavailable", config: defaultConfig, err: status.Error(codes.Unavailable, "offline"), want: true},
		{name: "default endpoint gRPC deadline", config: defaultConfig, err: status.Error(codes.DeadlineExceeded, "deadline"), want: true},
		{name: "default endpoint context deadline", config: defaultConfig, err: context.DeadlineExceeded, want: true},
		{name: "authentication failure", config: defaultConfig, err: status.Error(codes.Unauthenticated, "unauthenticated")},
		{name: "authorization failure", config: defaultConfig, err: status.Error(codes.PermissionDenied, "denied")},
		{name: "custom TLS", config: Options{APIURL: defaultAPIURL, GatewayURL: defaultGatewayURL, CAFile: "/ca.pem"}, err: status.Error(codes.Unavailable, "TLS failed")},
		{name: "explicit API endpoint", config: Options{APIURL: "https://api.example.test", GatewayURL: defaultGatewayURL}, err: status.Error(codes.Unavailable, "offline")},
		{name: "explicit gateway endpoint", config: Options{APIURL: defaultAPIURL, GatewayURL: "https://gateway.example.test"}, endpoint: "https://gateway.example.test", err: status.Error(codes.Unavailable, "offline")},
		{name: "other error", config: defaultConfig, err: errors.New("invalid CA")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := tt.endpoint
			if endpoint == "" {
				endpoint = tt.config.APIURL
			}
			assert.Equal(t, tt.want, shouldPortForward(&tt.config, endpoint, tt.err))
		})
	}
}

func TestBoundedBuffer(t *testing.T) {
	buffer := newBoundedBuffer(4)
	written, err := buffer.Write([]byte("abcdef"))
	require.NoError(t, err)
	assert.Equal(t, 6, written)
	assert.Equal(t, "abcd", buffer.String())
}
