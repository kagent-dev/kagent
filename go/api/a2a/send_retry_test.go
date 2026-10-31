package a2a

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/stretchr/testify/require"
)

func unacceptedSendError(meta map[string]string) error {
	return a2atype.NewError(a2atype.ErrUnsupportedOperation, "input was not accepted").WithErrorInfoMeta(meta)
}

func TestSendNotAcceptedRetryDelay(t *testing.T) {
	for _, tt := range []struct {
		name      string
		err       error
		wantDelay time.Duration
		wantOK    bool
	}{
		{name: "explicit rejection", err: unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason, "retryAfterMs": "250"}), wantDelay: 250 * time.Millisecond, wantOK: true},
		{name: "rejection without delay", err: unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason}), wantDelay: sendNotAcceptedDefaultDelay, wantOK: true},
		{name: "invalid delay", err: unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason, "retryAfterMs": "soon"}), wantDelay: sendNotAcceptedDefaultDelay, wantOK: true},
		{name: "wrapped rejection", err: errors.Join(errors.New("send"), unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason})), wantDelay: sendNotAcceptedDefaultDelay, wantOK: true},
		{name: "nil", err: nil},
		{name: "ambiguous transport failure", err: a2atype.ErrInternalError},
		{name: "wrong error with rejection metadata", err: a2atype.NewError(a2atype.ErrInternalError, "rejected").
			WithErrorInfoMeta(map[string]string{"reason": SendNotAcceptedReason})},
		{name: "unsupported operation", err: a2atype.ErrUnsupportedOperation},
		{name: "matching text without proof", err: errors.New("input was not accepted")},
		{name: "other domain", err: a2atype.NewError(a2atype.ErrUnsupportedOperation, "rejected").WithTypedDetails(
			errordetails.NewErrorInfo("UNSUPPORTED_OPERATION", "other.example", map[string]string{"reason": SendNotAcceptedReason}),
		)},
		{name: "other reason", err: unacceptedSendError(map[string]string{"reason": "OTHER_REASON"})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			delay, ok := SendNotAcceptedRetryDelay(tt.err)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantDelay, delay)
		})
	}
}

func TestSendNotAcceptedRetryDelayDoesNotMutateError(t *testing.T) {
	err := a2atype.NewError(a2atype.ErrUnsupportedOperation, "no details")
	_, ok := SendNotAcceptedRetryDelay(err)
	require.False(t, ok)
	require.Empty(t, err.TypedDetails)
}

func TestRetryUnacceptedSend(t *testing.T) {
	for _, tt := range []struct {
		name      string
		err       error
		wantCalls int
	}{
		{name: "explicit rejection", err: unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason, "retryAfterMs": "100"}), wantCalls: 2},
		{name: "ambiguous transport failure", err: a2atype.ErrInternalError, wantCalls: 1},
		{name: "other reason", err: unacceptedSendError(map[string]string{"reason": "OTHER_REASON"}), wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				want := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("answer"))
				result, err := RetryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
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

func TestRetryUnacceptedSendHonorsRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		_, err := RetryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
			if calls++; calls == 1 {
				return nil, unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason, "retryAfterMs": "2000"})
			}
			return a2atype.NewMessage(a2atype.MessageRoleAgent), nil
		})
		require.NoError(t, err)
		require.Equal(t, 2*time.Second, time.Since(start))
	})
}

func TestRetryUnacceptedSendStopsOnPartialResult(t *testing.T) {
	partial := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("partial response"))
	rejection := unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason})
	calls := 0
	result, err := RetryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
		calls++
		return partial, rejection
	})
	require.ErrorIs(t, err, rejection)
	require.Same(t, partial, result)
	require.Equal(t, 1, calls)
}

func TestRetryUnacceptedSendHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rejection := unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason})
	calls := 0
	_, err := RetryUnacceptedSend(ctx, func() (a2atype.SendMessageResult, error) {
		calls++
		cancel()
		return nil, rejection
	})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, rejection)
	require.Equal(t, 1, calls)
}

func TestRetryUnacceptedSendIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		rejection := unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason, "retryAfterMs": "7000"})
		_, err := RetryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
			return nil, rejection
		})
		require.ErrorIs(t, err, rejection)
		require.Equal(t, sendNotAcceptedRetryWindow, time.Since(start))
	})
}

func TestRetryUnacceptedSendDoesNotLimitAcceptedExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		want := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("answer"))
		result, err := RetryUnacceptedSend(t.Context(), func() (a2atype.SendMessageResult, error) {
			calls++
			if calls == 1 {
				return nil, unacceptedSendError(map[string]string{"reason": SendNotAcceptedReason})
			}
			time.Sleep(time.Minute)
			return want, nil
		})
		require.NoError(t, err)
		require.Same(t, want, result)
		require.Equal(t, 2, calls)
	})
}
