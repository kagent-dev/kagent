package a2a

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/errordetails"
)

// SendNotAcceptedReason is the ErrorInfo metadata reason the kagent A2A
// gateway attaches to a send it proved it never dispatched, so the same
// message may be sent again. See "Durable ordering" in
// docs/architecture/a2a-gateway.md.
const SendNotAcceptedReason = "KAGENT_SEND_NOT_ACCEPTED"

const (
	// sendNotAcceptedRetryWindow bounds how long a busy session can defer a
	// send. It limits retries, not the duration of an accepted send.
	sendNotAcceptedRetryWindow  = 30 * time.Second
	sendNotAcceptedDefaultDelay = 100 * time.Millisecond
)

// SendNotAcceptedRetryDelay reports whether err is a KAGENT_SEND_NOT_ACCEPTED
// rejection and, if so, how long to wait before resending, honoring the
// rejection's retryAfterMs.
func SendNotAcceptedRetryDelay(err error) (time.Duration, bool) {
	var protocolErr *a2atype.Error
	if !errors.Is(err, a2atype.ErrUnsupportedOperation) || !errors.As(err, &protocolErr) {
		return 0, false
	}
	// Not protocolErr.ErrorInfo(): it appends a default ErrorInfo to errors
	// that have none, mutating shared sentinel errors.
	i := slices.IndexFunc(protocolErr.TypedDetails, func(d *errordetails.Typed) bool {
		return d.TypeURL == errordetails.ErrorInfoType
	})
	if i < 0 {
		return 0, false
	}
	info := protocolErr.TypedDetails[i].Value
	metadata, _ := info["metadata"].(map[string]string)
	if info["domain"] != a2atype.ProtocolDomain || metadata["reason"] != SendNotAcceptedReason {
		return 0, false
	}
	if ms, err := strconv.Atoi(metadata["retryAfterMs"]); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond, true
	}
	return sendNotAcceptedDefaultDelay, true
}

// RetryUnacceptedSend calls send and calls it again, for up to 30 seconds,
// only while it fails with a KAGENT_SEND_NOT_ACCEPTED rejection. send must
// resend the identical message, keeping its ID. A partial result or any other
// error, including transport errors after which the remote agent may already
// have acted on the input, is returned unchanged.
func RetryUnacceptedSend(ctx context.Context, send func() (a2atype.SendMessageResult, error)) (a2atype.SendMessageResult, error) {
	deadline := time.Now().Add(sendNotAcceptedRetryWindow)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := send()
		delay, ok := SendNotAcceptedRetryDelay(err)
		if result != nil || !ok || !time.Now().Before(deadline) {
			return result, err
		}
		timer := time.NewTimer(min(delay, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(ctx.Err(), err)
		case <-timer.C:
			if !time.Now().Before(deadline) {
				return result, err
			}
		}
	}
}
