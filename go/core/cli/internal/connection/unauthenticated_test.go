package connection

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var errInvalidCredentials = status.Error(codes.Unauthenticated, "invalid credentials")

func TestExplainUnauthenticated(t *testing.T) {
	resolved := callerToken{value: "caller-jwt", source: "KAGENT_TOKEN"}
	tests := []struct {
		name     string
		token    callerToken
		outgoing metadata.MD
		err      error
		want     string
	}{
		{
			name: "no token names every source",
			err:  errInvalidCredentials,
			want: "invalid credentials: no caller token was sent; pass --caller-token, set KAGENT_TOKEN, or select a kubeconfig context whose credential is a token (an exec plugin or an id-token)",
		},
		{
			name:     "a sent token names its source",
			token:    resolved,
			outgoing: metadata.Pairs("authorization", "Bearer caller-jwt"),
			err:      errInvalidCredentials,
			want:     "invalid credentials: the caller token from KAGENT_TOKEN was refused",
		},
		{
			name:     "a model key in the slot names the conflict",
			token:    resolved,
			outgoing: metadata.Pairs("authorization", "Bearer model-key"),
			err:      errInvalidCredentials,
			want:     "invalid credentials: the call carried the model key of --token as its bearer in place of the caller token from KAGENT_TOKEN; a controller that identifies the caller by the bearer takes no model key there, so drop --token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &Options{token: tt.token}
			ctx := context.Background()
			if tt.outgoing != nil {
				ctx = metadata.NewOutgoingContext(ctx, tt.outgoing)
			}

			got := options.explainUnauthenticated(ctx, tt.err)
			require.Equal(t, codes.Unauthenticated, status.Code(got))
			assert.Equal(t, tt.want, status.Convert(got).Message())
		})
	}
}

func TestExplainUnauthenticatedLeavesOtherErrorsAlone(t *testing.T) {
	options := &Options{}
	denied := status.Error(codes.PermissionDenied, "denied")
	assert.Same(t, denied, options.explainUnauthenticated(context.Background(), denied))
	assert.NoError(t, options.explainUnauthenticated(context.Background(), nil))
	plain := errors.New("plain")
	assert.Same(t, plain, options.explainUnauthenticated(context.Background(), plain))
}

type refusingStream struct {
	grpc.ClientStream
}

func (refusingStream) RecvMsg(any) error {
	return errInvalidCredentials
}

func TestInterceptorsExplainOnTheCall(t *testing.T) {
	options := &Options{}
	ctx := context.Background()

	invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		return errInvalidCredentials
	}
	err := options.unaryInterceptor(ctx, "/kagent.api.v1alpha1.SessionService/CreateSession", nil, nil, nil, invoker)
	assert.Contains(t, status.Convert(err).Message(), "no caller token was sent")

	streamer := func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		return refusingStream{}, nil
	}
	stream, err := options.streamInterceptor(ctx, &grpc.StreamDesc{}, nil, "/a2a.v1.A2AService/SendStreamingMessage", streamer)
	require.NoError(t, err)
	assert.Contains(t, status.Convert(stream.RecvMsg(nil)).Message(), "no caller token was sent")
}
