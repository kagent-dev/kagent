package a2a

import (
	"encoding/json"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

const storedTaskMetadataKey = "https://kagent.dev/internal/stored-task/v1"

// ClearStoredTask removes the private gateway-to-runtime continuation state.
// Public callers may send this key, so the gateway must clear it before use.
func ClearStoredTask(message *a2atype.Message) {
	if message != nil {
		delete(message.Metadata, storedTaskMetadataKey)
	}
}

// AttachStoredTask carries the canonical waiting state to a newly started runtime.
func AttachStoredTask(message *a2atype.Message, task *a2atype.Task) error {
	ClearStoredTask(message)
	if message == nil || task == nil {
		return fmt.Errorf("stored waiting task is required")
	}
	var statusMessage map[string]any
	if task.Status.Message != nil {
		encoded, err := json.Marshal(task.Status.Message)
		if err != nil {
			return fmt.Errorf("encode stored status message: %w", err)
		}
		if err := json.Unmarshal(encoded, &statusMessage); err != nil {
			return fmt.Errorf("encode stored status message: %w", err)
		}
	}
	if message.Metadata == nil {
		message.Metadata = make(map[string]any)
	}
	stored := map[string]any{"state": string(task.Status.State)}
	if statusMessage != nil {
		stored["message"] = statusMessage
	}
	message.Metadata[storedTaskMetadataKey] = stored
	return nil
}

// TakeStoredTask consumes private continuation state before upstream A2A can
// persist it in task history.
func TakeStoredTask(message *a2atype.Message) (*a2atype.Task, error) {
	if message == nil || message.Metadata == nil {
		return nil, nil
	}
	raw, ok := message.Metadata[storedTaskMetadataKey]
	delete(message.Metadata, storedTaskMetadataKey)
	if !ok {
		return nil, nil
	}
	state, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid stored task state")
	}
	taskState, _ := state["state"].(string)
	if taskState != string(a2atype.TaskStateInputRequired) && taskState != string(a2atype.TaskStateAuthRequired) {
		return nil, fmt.Errorf("invalid stored task state %q", taskState)
	}
	var statusMessage *a2atype.Message
	if rawMessage, ok := state["message"]; ok && rawMessage != nil {
		encoded, err := json.Marshal(rawMessage)
		if err != nil {
			return nil, fmt.Errorf("decode stored status message: %w", err)
		}
		statusMessage = &a2atype.Message{}
		if err := json.Unmarshal(encoded, statusMessage); err != nil {
			return nil, fmt.Errorf("decode stored status message: %w", err)
		}
	}
	return &a2atype.Task{
		ID: message.TaskID, ContextID: message.ContextID,
		Status: a2atype.TaskStatus{State: a2atype.TaskState(taskState), Message: statusMessage},
	}, nil
}
