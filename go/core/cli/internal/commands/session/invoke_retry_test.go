package session

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/stretchr/testify/require"
	grpcdetails "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

type rejectingSendServer struct {
	a2apb.UnimplementedA2AServiceServer
	attempts atomic.Int32
	requests chan *a2apb.SendMessageRequest
	rejected error
	response *a2apb.SendMessageResponse
}

var _ a2apb.A2AServiceServer = (*rejectingSendServer)(nil)

func (r *rejectingSendServer) SendMessage(_ context.Context, request *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	r.requests <- request
	if r.attempts.Add(1) == 1 {
		return nil, r.rejected
	}
	return r.response, nil
}

func (r *rejectingSendServer) SendStreamingMessage(request *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	response, err := r.SendMessage(stream.Context(), request)
	if err != nil {
		return err
	}
	return stream.Send(&a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Task{Task: response.GetTask()}})
}

func TestInvokeRetriesRejectedGRPCSend(t *testing.T) {
	for _, mode := range []string{"table", "json", "table stream", "json stream", "push"} {
		t.Run(mode, func(t *testing.T) {
			rejected, err := status.New(codes.FailedPrecondition, "input was not accepted").WithDetails(&grpcdetails.ErrorInfo{
				Reason: a2atype.ErrorReason(a2atype.ErrUnsupportedOperation), Domain: a2atype.ProtocolDomain,
				Metadata: map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED", "retryAfterMs": "100"},
			})
			require.NoError(t, err)
			response, err := pbconv.ToProtoSendMessageResponse(&a2atype.Task{
				ID: "task", ContextID: "session", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
				Artifacts: []*a2atype.Artifact{{ID: "answer", Parts: a2atype.ContentParts{a2atype.NewTextPart("answer")}}},
			})
			require.NoError(t, err)
			backend := &rejectingSendServer{requests: make(chan *a2apb.SendMessageRequest, 2), rejected: rejected.Err(), response: response}
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			a2apb.RegisterA2AServiceServer(server, backend)
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			t.Cleanup(func() {
				server.Stop()
				require.NoError(t, <-done)
			})
			client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2atype.AgentInterface{{
				URL: "passthrough:///bufconn", ProtocolBinding: a2atype.TransportProtocolGRPC, ProtocolVersion: a2atype.Version,
			}}, a2agrpc.WithGRPCTransport(
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
			))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Destroy()) })
			request := newInvokeRequest("hello")
			var output bytes.Buffer
			switch mode {
			case "table":
				err = invokeNonStreaming(t.Context(), client, request, clioutput.FormatTable, &output)
			case "json":
				err = invokeNonStreaming(t.Context(), client, request, clioutput.FormatJSON, &output)
			case "table stream":
				err = invokeStreaming(t.Context(), client, request, clioutput.FormatTable, &output)
			case "json stream":
				err = invokeStreaming(t.Context(), client, request, clioutput.FormatJSON, &output)
			case "push":
				err = invokeWithPush(t.Context(), client, request, &InvokeCfg{PushURL: "https://receiver.example/callback"}, clioutput.FormatJSON, &output)
			}
			require.NoError(t, err)
			require.EqualValues(t, 2, backend.attempts.Load())
			first, second := <-backend.requests, <-backend.requests
			require.True(t, proto.Equal(first, second), "retry must preserve the message and callback IDs")
			require.Equal(t, request.Message.ID, second.GetMessage().GetMessageId())
			if mode == "table" || mode == "table stream" {
				require.Equal(t, "answer\n", output.String())
			} else {
				require.Contains(t, output.String(), "answer")
			}
		})
	}
}

func unacceptedSendError() error {
	return a2atype.NewError(a2atype.ErrUnsupportedOperation, "input was not accepted").
		WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED", "retryAfterMs": "100"})
}

func TestRetryUnacceptedSend(t *testing.T) {
	for _, tt := range []struct {
		name      string
		err       error
		wantCalls int
	}{
		{name: "explicit rejection", err: unacceptedSendError(), wantCalls: 2},
		{name: "ambiguous transport failure", err: a2atype.ErrInternalError, wantCalls: 1},
		{name: "wrong error with rejection metadata", err: a2atype.NewError(a2atype.ErrInternalError, "rejected").
			WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"}), wantCalls: 1},
		{name: "unsupported operation", err: a2atype.ErrUnsupportedOperation, wantCalls: 1},
		{name: "matching text without proof", err: errors.New("input was not accepted"), wantCalls: 1},
		{name: "other domain", err: a2atype.NewError(a2atype.ErrUnsupportedOperation, "rejected").WithTypedDetails(
			errordetails.NewErrorInfo("UNSUPPORTED_OPERATION", "other.example", map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"}),
		), wantCalls: 1},
		{name: "other reason", err: a2atype.NewError(a2atype.ErrUnsupportedOperation, "rejected").
			WithErrorInfoMeta(map[string]string{"reason": "OTHER_REASON"}), wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				want := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("answer"))
				result, err := retryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
					calls++
					if calls == 1 {
						return nil, tt.err
					}
					return want, nil
				})
				require.Equal(t, tt.wantCalls, calls)
				if tt.wantCalls == 1 {
					require.ErrorIs(t, err, tt.err)
					require.Nil(t, result)
				} else {
					require.NoError(t, err)
					require.Same(t, want, result)
				}
			})
		})
	}
}

func TestRetryUnacceptedSendPreservesPartialStream(t *testing.T) {
	var output bytes.Buffer
	writer := &tableStreamWriter{w: &output}
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("partial response"))
	rejection := unacceptedSendError()
	calls := 0
	result, err := retryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
		calls++
		return consumeA2AStream(eventStream(streamItem{event: message}, streamItem{err: rejection}), writer.Write)
	})
	require.ErrorIs(t, err, rejection)
	require.Same(t, message, result)
	require.Equal(t, 1, calls)
	require.Equal(t, "partial response", output.String())
}

func TestRetryUnacceptedSendHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	_, err := retryUnacceptedSend(ctx, func() (a2atype.SendMessageResult, error) {
		calls++
		cancel()
		return nil, unacceptedSendError()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

func TestRetryUnacceptedSendIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		rejection := unacceptedSendError()
		_, err := retryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
			return nil, rejection
		})
		require.ErrorIs(t, err, rejection)
		require.InDelta(t, 30, time.Since(start).Seconds(), 0.1)
	})
}

func TestRetryUnacceptedSendDoesNotLimitAcceptedExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		want := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("answer"))
		result, err := retryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
			calls++
			if calls == 1 {
				return nil, unacceptedSendError()
			}
			time.Sleep(time.Minute)
			return want, nil
		})
		require.NoError(t, err)
		require.Same(t, want, result)
		require.Equal(t, 2, calls)
	})
}
