package e2e_test

import (
	"context"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type retrySendClient struct {
	a2apb.A2AServiceClient
	send func(context.Context, *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error)
}

func (c retrySendClient) SendMessage(ctx context.Context, request *a2apb.SendMessageRequest, _ ...grpc.CallOption) (*a2apb.SendMessageResponse, error) {
	return c.send(ctx, request)
}

func TestSendMessageRetryContract(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   codes.Code
		domain string
		reason string
		retry  bool
	}{
		{name: "proven rejection", code: codes.FailedPrecondition, domain: a2atype.ProtocolDomain, reason: "KAGENT_SEND_NOT_ACCEPTED", retry: true},
		{name: "ambiguous transport failure", code: codes.Unavailable},
		{name: "internal error", code: codes.Internal},
		{name: "other precondition", code: codes.FailedPrecondition},
		{name: "wrong domain", code: codes.FailedPrecondition, domain: "other", reason: "KAGENT_SEND_NOT_ACCEPTED"},
		{name: "wrong status", code: codes.Internal, domain: a2atype.ProtocolDomain, reason: "KAGENT_SEND_NOT_ACCEPTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rejected, err := status.New(test.code, "send failed").WithDetails(&errdetails.ErrorInfo{
				Domain: test.domain, Metadata: map[string]string{"reason": test.reason, "retryAfterMs": "100"},
			})
			require.NoError(t, err)
			_, request := newMessageRequest(t, "What is 3+3?")
			request.Tenant, request.Message.ContextId = "kagent/assistant", "conversation"
			original := proto.Clone(request)
			response := &a2apb.SendMessageResponse{}
			calls := 0
			client := retrySendClient{send: func(_ context.Context, actual *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
				calls++
				require.True(t, proto.Equal(original, actual), "retry changed the request or its identity")
				if calls == 1 {
					return nil, rejected.Err()
				}
				return response, nil
			}}
			result, err := sendMessageWithRetry(t.Context(), client, request)
			if test.retry {
				require.NoError(t, err)
				require.Same(t, response, result)
				require.Equal(t, 2, calls)
			} else {
				require.ErrorIs(t, err, rejected.Err())
				require.Nil(t, result)
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestSendMessageRetryStopsAtDeadline(t *testing.T) {
	rejected, err := status.New(codes.FailedPrecondition, "not accepted").WithDetails(&errdetails.ErrorInfo{
		Domain: a2atype.ProtocolDomain, Metadata: map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	calls := 0
	client := retrySendClient{send: func(context.Context, *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
		calls++
		return nil, rejected.Err()
	}}
	_, request := newMessageRequest(t, "What is 3+3?")
	_, err = sendMessageWithRetry(ctx, client, request)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, calls)
}
