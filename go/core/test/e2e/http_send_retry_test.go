package e2e_test

import (
	"context"
	"errors"
	"iter"
	"net/http"
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
	send  func(context.Context, *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error]
	unary func(context.Context, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error)
}

var _ a2asrv.RequestHandler = retryHTTPStreamHandler{}

func (h retryHTTPStreamHandler) SendMessage(ctx context.Context, request *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	return h.unary(ctx, request)
}

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

func TestHTTPUnarySendRetryContract(t *testing.T) {
	rejected := a2atype.NewError(a2atype.ErrUnsupportedOperation, "not accepted").
		WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"})
	for _, test := range []struct {
		name  string
		err   error
		retry bool
	}{
		{name: "proven rejection", err: rejected, retry: true},
		{name: "ambiguous error", err: a2atype.ErrInternalError},
		{name: "other rejection", err: a2atype.ErrUnsupportedOperation},
		{name: "other reason", err: a2atype.NewError(a2atype.ErrUnsupportedOperation, "not accepted").WithErrorInfoMeta(map[string]string{"reason": "OTHER_REASON"})},
		{name: "wrong error", err: a2atype.NewError(a2atype.ErrInternalError, "not accepted").WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"})},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Initial sends omit ContextID; the gateway derives the conversation's
			// creation request ID from this message ID, so retries must preserve it.
			request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 3+3?"))}
			task := &a2atype.Task{ID: "task", ContextID: "conversation", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
			var mu sync.Mutex
			var requests []*a2atype.SendMessageRequest
			client := newHTTPRetryClient(t, retryHTTPStreamHandler{unary: func(_ context.Context, actual *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, actual)
				if len(requests) == 1 {
					return nil, test.err
				}
				return task, nil
			}})
			result, err := sendHTTPMessageWithRetry(t.Context(), client, request)
			wantCalls := 1
			if test.retry {
				require.NoError(t, err)
				require.Equal(t, task, result)
				wantCalls = 2
			} else {
				require.Error(t, err)
				require.Equal(t, a2atype.ErrorReason(test.err), a2atype.ErrorReason(err))
				require.Nil(t, result)
			}
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, requests, wantCalls)
			for _, actual := range requests {
				require.Equal(t, request, actual, "retry changed the request or its identity")
			}
		})
	}
}

func TestHTTPUnarySendRetryStopsAtDeadline(t *testing.T) {
	client := newHTTPRetryClient(t, retryHTTPStreamHandler{unary: func(context.Context, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "not accepted").
			WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"})
	}})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 3+3?"))}
	result, err := sendHTTPMessageWithRetry(ctx, client, request)
	require.Nil(t, result)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

type retryHTTPDeadlineTransport struct {
	t        *testing.T
	deadline time.Time
	err      error
}

var _ http.RoundTripper = retryHTTPDeadlineTransport{}

func (r retryHTTPDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	deadline, ok := request.Context().Deadline()
	require.True(r.t, ok)
	require.Equal(r.t, r.deadline, deadline, "retry budget shortened the request deadline")
	return nil, r.err
}

func TestHTTPSendRetryPreservesDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	wantErr := errors.New("transport failed")
	client, err := a2aclient.NewFromEndpoints(ctx, []*a2atype.AgentInterface{{
		URL: "http://agent.invalid", ProtocolBinding: a2atype.TransportProtocolJSONRPC, ProtocolVersion: a2atype.Version,
	}}, a2aclient.WithJSONRPCTransport(&http.Client{Transport: retryHTTPDeadlineTransport{t: t, deadline: deadline, err: wantErr}}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Destroy()) })
	request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 3+3?"))}
	_, err = sendHTTPMessageWithRetry(ctx, client, request)
	require.ErrorIs(t, err, wantErr)
	var streamErr error
	for _, err := range sendHTTPStreamingMessageWithRetry(ctx, client, request) {
		streamErr = err
	}
	require.ErrorIs(t, streamErr, wantErr)
}
