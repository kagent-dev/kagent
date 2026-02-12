package core

import (
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// TaskResultAggregator aggregates parts from A2A events and maintains the final task state.
// For TaskStateWorking it accumulates parts from each status-update so the final artifact
// includes all content (text, function_call, function_response) for the UI results section.
type TaskResultAggregator struct {
	TaskState        protocol.TaskState
	TaskMessage      *protocol.Message
	accumulatedParts []protocol.Part
}

// NewTaskResultAggregator creates a new TaskResultAggregator.
func NewTaskResultAggregator() *TaskResultAggregator {
	return &TaskResultAggregator{
		TaskState:        protocol.TaskStateWorking,
		accumulatedParts: nil,
	}
}

// ProcessEvent processes an A2A event and updates the aggregated state.
func (a *TaskResultAggregator) ProcessEvent(event protocol.Event) {
	if statusUpdate, ok := event.(*protocol.TaskStatusUpdateEvent); ok {
		if statusUpdate.Status.State == protocol.TaskStateFailed {
			a.TaskState = protocol.TaskStateFailed
			a.TaskMessage = statusUpdate.Status.Message
		} else if statusUpdate.Status.State == protocol.TaskStateAuthRequired && a.TaskState != protocol.TaskStateFailed {
			a.TaskState = protocol.TaskStateAuthRequired
			a.TaskMessage = statusUpdate.Status.Message
		} else if statusUpdate.Status.State == protocol.TaskStateInputRequired &&
			a.TaskState != protocol.TaskStateFailed &&
			a.TaskState != protocol.TaskStateAuthRequired {
			a.TaskState = protocol.TaskStateInputRequired
			a.TaskMessage = statusUpdate.Status.Message
		} else if a.TaskState == protocol.TaskStateWorking {
			// Accumulate parts so final artifact has full content (text + tool calls + tool results)
			// for the UI results section (matching Python packages behavior).
			if statusUpdate.Status.Message != nil && len(statusUpdate.Status.Message.Parts) > 0 {
				a.accumulatedParts = append(a.accumulatedParts, statusUpdate.Status.Message.Parts...)
				a.TaskMessage = &protocol.Message{
					MessageID: uuid.New().String(),
					Role:      protocol.MessageRoleAgent,
					Parts:     append([]protocol.Part(nil), a.accumulatedParts...),
				}
			} else {
				a.TaskMessage = statusUpdate.Status.Message
			}
		}
		// In A2A, we often want to keep the event state as "working" for intermediate updates
		// to avoid prematurely terminating the event stream in the handler.
		statusUpdate.Status.State = protocol.TaskStateWorking
	}
}
