package a2a

import (
	"errors"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
)

// Assembler projects an A2A event stream into its current Message or Task.
type Assembler struct {
	message *a2atype.Message
	task    *a2atype.Task
}

// Apply incorporates one A2A event into the assembled result.
func (a *Assembler) Apply(event a2atype.Event) error {
	if event == nil {
		return errors.New("a2a stream returned a nil event")
	}
	if message, ok := event.(*a2atype.Message); ok {
		if a.message != nil || a.task != nil {
			return errors.New("a2a stream returned a Message after another result")
		}
		a.message = message
		return nil
	}
	if a.message != nil {
		return errors.New("a2a stream returned a task event after a Message")
	}
	if a.task == nil {
		info := event.TaskInfo()
		if info.TaskID == "" || info.ContextID == "" {
			return errors.New("a2a task event is missing task or context identity")
		}
		a.task = &a2atype.Task{
			ID:        info.TaskID,
			ContextID: info.ContextID,
			Status:    a2atype.TaskStatus{State: a2atype.TaskStateSubmitted},
		}
	}

	updated, err := a2aevent.ApplyUpdate(a.task, event)
	if err != nil {
		return fmt.Errorf("apply a2a event: %w", err)
	}
	a.task = updated
	return nil
}

// Result returns the assembled result, if the stream has produced one.
func (a *Assembler) Result() a2atype.SendMessageResult {
	if a.message != nil {
		return a.message
	}
	if a.task != nil {
		return a.task
	}
	return nil
}

// Complete reports whether the assembled result is ready to return to the caller.
func (a *Assembler) Complete() bool {
	if a.message != nil {
		return true
	}
	if a.task == nil {
		return false
	}
	state := a.task.Status.State
	return state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}
