package e2e_test

import (
	"context"
	"io"
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
	send   func(context.Context, *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error)
	stream func(context.Context, *a2apb.SendMessageRequest) (grpc.ServerStreamingClient[a2apb.StreamResponse], error)
}

var _ a2apb.A2AServiceClient = retrySendClient{}

func (c retrySendClient) SendMessage(ctx context.Context, request *a2apb.SendMessageRequest, _ ...grpc.CallOption) (*a2apb.SendMessageResponse, error) {
	return c.send(ctx, request)
}

func (c retrySendClient) SendStreamingMessage(ctx context.Context, request *a2apb.SendMessageRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[a2apb.StreamResponse], error) {
	return c.stream(ctx, request)
}

type retrySendStream struct {
	grpc.ClientStream
	recv func() (*a2apb.StreamResponse, error)
}

var _ grpc.ServerStreamingClient[a2apb.StreamResponse] = retrySendStream{}

func (s retrySendStream) Recv() (*a2apb.StreamResponse, error) {
	return s.recv()
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
			client := retrySendClient{send: func(ctx context.Context, actual *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
				calls++
				require.Equal(t, t.Context(), ctx, "retry budget replaced the caller's context")
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

func TestStreamingSendRetryContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		code       codes.Code
		domain     string
		reason     string
		openError  bool
		afterEvent bool
		retry      bool
	}{
		{name: "rejection on first receive", code: codes.FailedPrecondition, domain: a2atype.ProtocolDomain, reason: "KAGENT_SEND_NOT_ACCEPTED", retry: true},
		{name: "rejection opening stream", code: codes.FailedPrecondition, domain: a2atype.ProtocolDomain, reason: "KAGENT_SEND_NOT_ACCEPTED", openError: true, retry: true},
		{name: "ambiguous transport failure", code: codes.Unavailable},
		{name: "ambiguous opening failure", code: codes.Unavailable, openError: true},
		{name: "other precondition", code: codes.FailedPrecondition},
		{name: "wrong domain", code: codes.FailedPrecondition, domain: "other", reason: "KAGENT_SEND_NOT_ACCEPTED"},
		{name: "wrong reason", code: codes.FailedPrecondition, domain: a2atype.ProtocolDomain, reason: "other"},
		{name: "wrong status", code: codes.Internal, domain: a2atype.ProtocolDomain, reason: "KAGENT_SEND_NOT_ACCEPTED"},
		{name: "rejection after event", code: codes.FailedPrecondition, domain: a2atype.ProtocolDomain, reason: "KAGENT_SEND_NOT_ACCEPTED", afterEvent: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rejected, err := status.New(test.code, "send failed").WithDetails(&errdetails.ErrorInfo{
				Domain: test.domain, Metadata: map[string]string{"reason": test.reason},
			})
			require.NoError(t, err)
			_, request := newMessageRequest(t, "What is 3+3?")
			request.Tenant, request.Message.ContextId = "kagent/assistant", "conversation"
			original := proto.Clone(request)
			response := &a2apb.StreamResponse{}
			calls := 0
			var attempts []context.Context
			client := retrySendClient{stream: func(ctx context.Context, actual *a2apb.SendMessageRequest) (grpc.ServerStreamingClient[a2apb.StreamResponse], error) {
				calls++
				attempts = append(attempts, ctx)
				require.True(t, proto.Equal(original, actual), "retry changed the request or its identity")
				if calls == 1 && test.openError {
					return nil, rejected.Err()
				}
				receives := 0
				return retrySendStream{recv: func() (*a2apb.StreamResponse, error) {
					receives++
					if calls == 1 && (!test.afterEvent || receives > 1) {
						return nil, rejected.Err()
					}
					if receives == 1 {
						return response, nil
					}
					return nil, io.EOF
				}}, nil
			}}
			var responses []*a2apb.StreamResponse
			var streamErr error
			for result, err := range sendStreamingMessageWithRetry(t.Context(), client, request) {
				if err != nil {
					streamErr = err
					continue
				}
				responses = append(responses, result)
			}
			if test.retry {
				require.NoError(t, streamErr)
				require.Equal(t, 2, calls)
			} else {
				require.ErrorIs(t, streamErr, rejected.Err())
				require.Equal(t, 1, calls)
			}
			if test.retry || test.afterEvent {
				require.Equal(t, []*a2apb.StreamResponse{response}, responses)
			} else {
				require.Empty(t, responses)
			}
			for _, attempt := range attempts {
				require.ErrorIs(t, attempt.Err(), context.Canceled, "stream was not closed")
			}
		})
	}
}

func TestStreamingSendRetryStopsAtDeadline(t *testing.T) {
	rejected, err := status.New(codes.FailedPrecondition, "not accepted").WithDetails(&errdetails.ErrorInfo{
		Domain: a2atype.ProtocolDomain, Metadata: map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	calls := 0
	client := retrySendClient{stream: func(context.Context, *a2apb.SendMessageRequest) (grpc.ServerStreamingClient[a2apb.StreamResponse], error) {
		calls++
		return retrySendStream{recv: func() (*a2apb.StreamResponse, error) { return nil, rejected.Err() }}, nil
	}}
	_, request := newMessageRequest(t, "What is 3+3?")
	var streamErr error
	for _, err := range sendStreamingMessageWithRetry(ctx, client, request) {
		streamErr = err
	}
	require.ErrorIs(t, streamErr, context.DeadlineExceeded)
	require.Equal(t, 1, calls)
}

func TestStreamingSendConsumerStops(t *testing.T) {
	var streamCtx context.Context
	calls, receives := 0, 0
	client := retrySendClient{stream: func(ctx context.Context, _ *a2apb.SendMessageRequest) (grpc.ServerStreamingClient[a2apb.StreamResponse], error) {
		calls++
		streamCtx = ctx
		return retrySendStream{recv: func() (*a2apb.StreamResponse, error) {
			receives++
			return &a2apb.StreamResponse{}, nil
		}}, nil
	}}
	_, request := newMessageRequest(t, "What is 3+3?")
	for _, err := range sendStreamingMessageWithRetry(t.Context(), client, request) {
		require.NoError(t, err)
		break
	}
	require.Equal(t, 1, calls)
	require.Equal(t, 1, receives)
	require.ErrorIs(t, streamCtx.Err(), context.Canceled)
}
