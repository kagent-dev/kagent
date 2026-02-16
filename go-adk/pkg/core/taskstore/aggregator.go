package taskstore

import (
	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
)

// TaskResultAggregator aggregates parts from A2A events and maintains the final task state.
// For TaskStateWorking it accumulates parts from each status-update so the final artifact
// includes all content (text, function_call, function_response) for the UI results section.
type TaskResultAggregator struct {
	TaskState        a2atype.TaskState
	TaskMessage      *a2atype.Message
	accumulatedParts []a2atype.Part
}

// NewTaskResultAggregator creates a new TaskResultAggregator.
func NewTaskResultAggregator() *TaskResultAggregator {
	return &TaskResultAggregator{
		TaskState:        a2atype.TaskStateWorking,
		accumulatedParts: nil,
	}
}

// ProcessEvent processes an A2A event and updates the aggregated state.
//
// Side effect: For TaskStatusUpdateEvent, this method mutates the event's Status.State
// to TaskStateWorking regardless of its original state. This is intentional: intermediate
// status updates are kept as "working" so the A2A event stream handler does not
// prematurely terminate the stream. The aggregator tracks the true final state internally
// (in a.TaskState) for use when building the terminal event after the runner completes.
func (a *TaskResultAggregator) ProcessEvent(event a2atype.Event) {
	statusUpdate, ok := event.(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		return
	}

	switch statusUpdate.Status.State {
	case a2atype.TaskStateFailed:
		a.TaskState = a2atype.TaskStateFailed
		a.TaskMessage = statusUpdate.Status.Message

	case a2atype.TaskStateAuthRequired:
		if a.TaskState != a2atype.TaskStateFailed {
			a.TaskState = a2atype.TaskStateAuthRequired
			a.TaskMessage = statusUpdate.Status.Message
		}

	case a2atype.TaskStateInputRequired:
		if a.TaskState != a2atype.TaskStateFailed && a.TaskState != a2atype.TaskStateAuthRequired {
			a.TaskState = a2atype.TaskStateInputRequired
			a.TaskMessage = statusUpdate.Status.Message
		}

	default:
		if a.TaskState == a2atype.TaskStateWorking {
			// Accumulate parts so final artifact has full content (text + tool calls + tool results)
			// for the UI results section (matching Python packages behavior).
			if statusUpdate.Status.Message != nil && len(statusUpdate.Status.Message.Parts) > 0 {
				a.accumulatedParts = append(a.accumulatedParts, statusUpdate.Status.Message.Parts...)
				a.TaskMessage = &a2atype.Message{
					ID:    uuid.New().String(),
					Role:  a2atype.MessageRoleAgent,
					Parts: append([]a2atype.Part(nil), a.accumulatedParts...),
				}
			} else {
				a.TaskMessage = statusUpdate.Status.Message
			}
		}
	}

	// In A2A, we often want to keep the event state as "working" for intermediate updates
	// to avoid prematurely terminating the event stream in the handler.
	statusUpdate.Status = a2atype.TaskStatus{
		State:     a2atype.TaskStateWorking,
		Message:   statusUpdate.Status.Message,
		Timestamp: statusUpdate.Status.Timestamp,
	}
}
