// Package server provides A2A server components including task management and event queues.
package server

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/taskstore"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// InMemoryEventQueue stores events in memory.
type InMemoryEventQueue struct {
	Events []protocol.Event
}

// EnqueueEvent adds an event to the in-memory queue.
func (q *InMemoryEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	q.Events = append(q.Events, event)
	return nil
}

// StreamingEventQueue streams events to a channel.
type StreamingEventQueue struct {
	Ch chan protocol.StreamingMessageEvent
}

// EnqueueEvent sends an event to the streaming channel.
func (q *StreamingEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	var streamEvent protocol.StreamingMessageEvent
	if statusEvent, ok := event.(*protocol.TaskStatusUpdateEvent); ok {
		streamEvent = protocol.StreamingMessageEvent{
			Result: statusEvent,
		}
	} else if artifactEvent, ok := event.(*protocol.TaskArtifactUpdateEvent); ok {
		streamEvent = protocol.StreamingMessageEvent{
			Result: artifactEvent,
		}
	} else {
		return fmt.Errorf("unsupported event type: %T", event)
	}

	select {
	case q.Ch <- streamEvent:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TaskSavingEventQueue wraps an EventQueue and automatically saves tasks to task store
// after each event is enqueued (matching Python A2A SDK behavior).
// contextID (session ID) is set on the task so GET /api/sessions/:id/tasks returns it.
type TaskSavingEventQueue struct {
	inner       a2a.EventQueue
	taskStore   *taskstore.KAgentTaskStore
	taskID      string
	contextID   string // session ID so tasks show in UI session tasks list
	logger      logr.Logger
	currentTask *protocol.Task // in-memory task to avoid overwriting with stale data
}

// NewTaskSavingEventQueue creates a new task-saving event queue.
func NewTaskSavingEventQueue(inner a2a.EventQueue, taskStore *taskstore.KAgentTaskStore, taskID, contextID string, logger logr.Logger) *TaskSavingEventQueue {
	return &TaskSavingEventQueue{
		inner:     inner,
		taskStore: taskStore,
		taskID:    taskID,
		contextID: contextID,
		logger:    logger,
	}
}

// EnqueueEvent enqueues an event and saves the task state.
func (q *TaskSavingEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	if err := q.inner.EnqueueEvent(ctx, event); err != nil {
		return err
	}
	if q.taskStore == nil {
		return nil
	}
	task := q.loadOrCreateTask(ctx)
	task.ContextID = q.contextID
	applyEventToTask(task, event)
	if err := q.taskStore.Save(ctx, task); err != nil {
		if q.logger.GetSink() != nil {
			q.logger.Error(err, "Failed to save task after enqueueing event", "taskID", q.taskID, "eventType", fmt.Sprintf("%T", event))
		}
	} else if q.logger.GetSink() != nil {
		q.logger.V(1).Info("Saved task after enqueueing event", "taskID", q.taskID, "eventType", fmt.Sprintf("%T", event))
	}
	return nil
}

func (q *TaskSavingEventQueue) loadOrCreateTask(ctx context.Context) *protocol.Task {
	if q.currentTask != nil {
		return q.currentTask
	}
	loaded, err := q.taskStore.Get(ctx, q.taskID)
	if err != nil || loaded == nil {
		q.currentTask = &protocol.Task{ID: q.taskID, ContextID: q.contextID}
	} else {
		q.currentTask = loaded
	}
	return q.currentTask
}

func applyEventToTask(task *protocol.Task, event protocol.Event) {
	if statusEvent, ok := event.(*protocol.TaskStatusUpdateEvent); ok {
		task.Status = statusEvent.Status
		if statusEvent.Status.Message != nil {
			if task.History == nil {
				task.History = []protocol.Message{}
			}
			task.History = append(task.History, *statusEvent.Status.Message)
		}
		return
	}
	if artifactEvent, ok := event.(*protocol.TaskArtifactUpdateEvent); ok && len(artifactEvent.Artifact.Parts) > 0 {
		if task.History == nil {
			task.History = []protocol.Message{}
		}
		task.History = append(task.History, protocol.Message{
			Kind:      protocol.KindMessage,
			MessageID: uuid.New().String(),
			Role:      protocol.MessageRoleAgent,
			Parts:     artifactEvent.Artifact.Parts,
		})
	}
}
