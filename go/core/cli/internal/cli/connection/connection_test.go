package connection

import (
	"context"
	"errors"
	"testing"

	"github.com/kagent-dev/kagent/go/api/client"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failingVersionClient struct {
	err error
}

func (c failingVersionClient) GetVersion(context.Context) (*api.VersionResponse, error) {
	return nil, c.err
}

func TestCheckServerPreservesCause(t *testing.T) {
	permissionErr := status.Error(codes.PermissionDenied, "denied")
	err := checkServer(t.Context(), &client.ClientSet{Version: failingVersionClient{err: permissionErr}})

	require.Error(t, err)
	assert.ErrorIs(t, err, errServerConnection)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestOptionsValidate(t *testing.T) {
	assert.NoError(t, (&Options{UserID: "user@example.com"}).validate())
	assert.Error(t, (&Options{}).validate())
	assert.Error(t, (&Options{UserID: "invalid user"}).validate())
}

func TestShouldPortForward(t *testing.T) {
	defaultConfig := Options{KAgentURL: defaultKAgentURL, KAgentGRPCURL: defaultKAgentGRPCURL}
	tests := []struct {
		name   string
		config Options
		err    error
		want   bool
	}{
		{name: "default endpoint unavailable", config: defaultConfig, err: status.Error(codes.Unavailable, "offline"), want: true},
		{name: "default endpoint gRPC deadline", config: defaultConfig, err: status.Error(codes.DeadlineExceeded, "deadline"), want: true},
		{name: "default endpoint context deadline", config: defaultConfig, err: context.DeadlineExceeded, want: true},
		{name: "empty gRPC endpoint uses client default", config: Options{KAgentURL: defaultKAgentURL}, err: status.Error(codes.Unavailable, "offline"), want: true},
		{name: "authentication failure", config: defaultConfig, err: status.Error(codes.Unauthenticated, "unauthenticated")},
		{name: "authorization failure", config: defaultConfig, err: status.Error(codes.PermissionDenied, "denied")},
		{name: "explicit TLS", config: Options{KAgentURL: defaultKAgentURL, KAgentGRPCURL: defaultKAgentGRPCURL, KAgentGRPCTLS: true}, err: status.Error(codes.Unavailable, "TLS failed")},
		{name: "explicit gRPC endpoint", config: Options{KAgentURL: defaultKAgentURL, KAgentGRPCURL: "api.example.test:443"}, err: status.Error(codes.Unavailable, "offline")},
		{name: "explicit HTTP endpoint", config: Options{KAgentURL: "https://api.example.test", KAgentGRPCURL: defaultKAgentGRPCURL}, err: status.Error(codes.Unavailable, "offline")},
		{name: "other error", config: defaultConfig, err: errors.New("invalid CA")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldPortForward(&tt.config, tt.err))
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
