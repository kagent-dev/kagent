package tools

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// sendNotAcceptedReason marks a send the kagent A2A gateway proved it never
// dispatched, so the same message may be sent again. See "Durable ordering"
// in docs/architecture/a2a-gateway.md.
const sendNotAcceptedReason = "KAGENT_SEND_NOT_ACCEPTED"

const (
	// sendNotAcceptedRetryTimeout bounds how long a busy session can defer a
	// send, matching the gateway's own client contract tests.
	sendNotAcceptedRetryTimeout = 30 * time.Second
	sendNotAcceptedDefaultDelay = 100 * time.Millisecond
)

// sendMessageWithRetry sends req and resends the identical request, keeping
// its message ID, only while the server reports KAGENT_SEND_NOT_ACCEPTED.
// Every other error, including transport errors after which the remote agent
// may already have acted on the input, is returned to the caller unchanged.
func sendMessageWithRetry(ctx context.Context, client *a2aclient.Client, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	deadline := time.Now().Add(sendNotAcceptedRetryTimeout)
	for {
		result, err := client.SendMessage(ctx, req)
		delay, ok := sendNotAcceptedRetryDelay(err)
		if !ok || time.Until(deadline) < delay {
			return result, err
		}
		logging.FromContext(ctx).InfoContext(ctx, "remote agent did not accept the send, retrying",
			"message_id", req.Message.ID, "retry_after", delay)
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(delay):
		}
	}
}

// sendNotAcceptedRetryDelay reports whether err is a KAGENT_SEND_NOT_ACCEPTED
// rejection and, if so, how long to wait before resending.
func sendNotAcceptedRetryDelay(err error) (time.Duration, bool) {
	var protocolErr *a2atype.Error
	if !errors.Is(err, a2atype.ErrUnsupportedOperation) || !errors.As(err, &protocolErr) {
		return 0, false
	}
	i := slices.IndexFunc(protocolErr.TypedDetails, func(d *errordetails.Typed) bool {
		return d.TypeURL == errordetails.ErrorInfoType
	})
	if i < 0 {
		return 0, false
	}
	info := protocolErr.TypedDetails[i].Value
	metadata, _ := info["metadata"].(map[string]string)
	if info["domain"] != a2atype.ProtocolDomain || metadata["reason"] != sendNotAcceptedReason {
		return 0, false
	}
	if ms, err := strconv.Atoi(metadata["retryAfterMs"]); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond, true
	}
	return sendNotAcceptedDefaultDelay, true
}

// recoverResumedTask looks up the task a resume message was sent to after
// the send failed ambiguously. It returns the task only when its history
// shows messageID was accepted and the task has since reached a state the
// caller can report: terminal, or waiting for input again. A task still
// working is followed with SubscribeToTask until it settles. Otherwise the
// outcome stays unknown and the caller reports the original error.
func recoverResumedTask(ctx context.Context, client *a2aclient.Client, taskID a2atype.TaskID, messageID string) (*a2atype.Task, bool) {
	task, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: taskID})
	if err != nil || !taskHasMessage(task, messageID) {
		return nil, false
	}
	if isSettledTaskState(task.Status.State) {
		return task, true
	}
	for _, err := range client.SubscribeToTask(ctx, &a2atype.SubscribeToTaskRequest{ID: taskID}) {
		if err != nil {
			break
		}
	}
	task, err = client.GetTask(ctx, &a2atype.GetTaskRequest{ID: taskID})
	if err != nil || !isSettledTaskState(task.Status.State) {
		return nil, false
	}
	return task, true
}

func taskHasMessage(task *a2atype.Task, messageID string) bool {
	return task != nil && slices.ContainsFunc(task.History, func(m *a2atype.Message) bool {
		return m != nil && m.ID == messageID
	})
}

func isSettledTaskState(state a2atype.TaskState) bool {
	return state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}
