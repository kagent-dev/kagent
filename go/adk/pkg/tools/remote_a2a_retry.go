package tools

import (
	"context"
	"net/http"
	"slices"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// sendMessageWithRetry sends req and resends the identical request, keeping
// its message ID, only while the server reports KAGENT_SEND_NOT_ACCEPTED.
// Every other error, including transport errors after which the remote agent
// may already have acted on the input, is returned to the caller unchanged.
func sendMessageWithRetry(ctx context.Context, client *a2aclient.Client, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	attempt := 0
	return kagenta2a.RetryUnacceptedSend(ctx, func() (a2atype.SendMessageResult, error) {
		if attempt++; attempt > 1 {
			logging.FromContext(ctx).InfoContext(ctx, "remote agent did not accept the send, retrying",
				"message_id", req.Message.ID, "attempt", attempt)
		}
		return client.SendMessage(ctx, req)
	})
}

// defaultResumeRecoveryTimeout bounds recoverResumedTask when the remote
// agent has no configured timeout.
const defaultResumeRecoveryTimeout = 5 * time.Minute

// resumeRecoveryTimeout returns the remote agent's configured timeout, or
// defaultResumeRecoveryTimeout when it has none.
func resumeRecoveryTimeout(c *http.Client) time.Duration {
	if c != nil && c.Timeout > 0 {
		return c.Timeout
	}
	return defaultResumeRecoveryTimeout
}

// recoverResumedTask looks up the task a resume message was sent to after
// the send failed ambiguously. It returns the task only when its history
// shows messageID was accepted and the task has since reached a state the
// caller can report: terminal, or waiting for input again. A task still
// working is followed with SubscribeToTask until it settles. Recovery gives
// up after timeout. Otherwise the outcome stays unknown and the caller
// reports the original error.
func recoverResumedTask(ctx context.Context, client *a2aclient.Client, taskID a2atype.TaskID, messageID string, timeout time.Duration) (*a2atype.Task, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
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
