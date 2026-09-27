package e2e_test

import (
	"context"
	"iter"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
)

type retryHTTPStreamHandler struct {
	a2asrv.RequestHandler
	send func(context.Context, *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error]
}

var _ a2asrv.RequestHandler = retryHTTPStreamHandler{}

func (h retryHTTPStreamHandler) SendStreamingMessage(ctx context.Context, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return h.send(ctx, request)
}

func newHTTPRetryClient(t *testing.T, handler retryHTTPStreamHandler) *a2aclient.Client {
	t.Helper()
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(handler))
	t.Cleanup(server.Close)
	client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2atype.AgentInterface{{
		URL: server.URL, ProtocolBinding: a2atype.TransportProtocolJSONRPC, ProtocolVersion: a2atype.Version,
	}}, a2aclient.WithJSONRPCTransport(server.Client()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Destroy()) })
	return client
}

func TestHTTPStreamingSendRetryContract(t *testing.T) {
	rejected := a2atype.NewError(a2atype.ErrUnsupportedOperation, "not accepted").
		WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED", "retryAfterMs": "100"})
	for _, test := range []struct {
		name       string
		err        error
		afterEvent bool
		stopEarly  bool
		retry      bool
	}{
		{name: "proven rejection", err: rejected, retry: true},
		{name: "ambiguous error", err: a2atype.ErrInternalError},
		{name: "other rejection", err: a2atype.ErrUnsupportedOperation},
		{name: "other reason", err: a2atype.NewError(a2atype.ErrUnsupportedOperation, "not accepted").WithErrorInfoMeta(map[string]string{"reason": "OTHER_REASON"})},
		{name: "wrong error", err: a2atype.NewError(a2atype.ErrInternalError, "not accepted").WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"})},
		{name: "rejection after event", err: rejected, afterEvent: true},
		{name: "consumer stops", err: rejected, afterEvent: true, stopEarly: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 3+3?"))}
			request.Message.ContextID = "conversation"
			task := &a2atype.Task{ID: "task", ContextID: "conversation", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}
			var mu sync.Mutex
			var requests []*a2atype.SendMessageRequest
			client := newHTTPRetryClient(t, retryHTTPStreamHandler{send: func(_ context.Context, actual *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
				return func(yield func(a2atype.Event, error) bool) {
					mu.Lock()
					requests = append(requests, actual)
					first := len(requests) == 1
					mu.Unlock()
					if first {
						if test.afterEvent && !yield(task, nil) {
							return
						}
						yield(nil, test.err)
						return
					}
					yield(task, nil)
				}
			}})
			var events []a2atype.Event
			var streamErr error
			for event, err := range sendHTTPStreamingMessageWithRetry(t.Context(), client, request) {
				if err != nil {
					streamErr = err
					break
				}
				events = append(events, event)
				if test.stopEarly {
					break
				}
			}
			if test.retry || test.stopEarly {
				require.NoError(t, streamErr)
			} else {
				require.Error(t, streamErr)
				require.Equal(t, a2atype.ErrorReason(test.err), a2atype.ErrorReason(streamErr))
			}
			if test.retry || test.afterEvent {
				require.Equal(t, []a2atype.Event{task}, events)
			} else {
				require.Empty(t, events)
			}
			mu.Lock()
			defer mu.Unlock()
			wantCalls := 1
			if test.retry {
				wantCalls = 2
			}
			require.Len(t, requests, wantCalls)
			for _, actual := range requests {
				require.Equal(t, request, actual, "retry changed the request or its identity")
			}
		})
	}
}

func TestHTTPStreamingSendRetryStopsAtDeadline(t *testing.T) {
	client := newHTTPRetryClient(t, retryHTTPStreamHandler{send: func(context.Context, *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
		return func(yield func(a2atype.Event, error) bool) {
			yield(nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "not accepted").
				WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"}))
		}
	}})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 3+3?"))}
	var streamErr error
	for event, err := range sendHTTPStreamingMessageWithRetry(ctx, client, request) {
		require.Nil(t, event)
		streamErr = err
	}
	require.ErrorIs(t, streamErr, context.DeadlineExceeded)
}
