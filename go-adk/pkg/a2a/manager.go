package a2a

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/taskstore"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// resolveContextID returns the session/context ID from the message for event persistence.
// Prefer message.ContextID (A2A contextId), then message.Metadata[kagent_session_id], then
// metadata contextId/context_id. Returns nil if none set so caller can generate one.
func resolveContextID(msg *protocol.Message) *string {
	if msg == nil {
		return nil
	}
	if msg.ContextID != nil && *msg.ContextID != "" {
		return msg.ContextID
	}
	if msg.Metadata != nil {
		for _, key := range []string{GetKAgentMetadataKey(MetadataKeySessionID), "contextId", "context_id"} {
			if v, ok := msg.Metadata[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return &s
				}
			}
		}
	}
	return nil
}

// ADKTaskManager implements taskmanager.TaskManager using the A2aAgentExecutor
type ADKTaskManager struct {
	executor              *A2aAgentExecutor
	taskStore             *taskstore.KAgentTaskStore
	pushNotificationStore *taskstore.KAgentPushNotificationStore
}

func NewADKTaskManager(executor *A2aAgentExecutor, taskStore *taskstore.KAgentTaskStore, pushNotificationStore *taskstore.KAgentPushNotificationStore) taskmanager.TaskManager {
	return &ADKTaskManager{
		executor:              executor,
		taskStore:             taskStore,
		pushNotificationStore: pushNotificationStore,
	}
}

func (m *ADKTaskManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	contextID := resolveContextID(&request.Message)
	if contextID == nil || *contextID == "" {
		contextIDString := uuid.New().String()
		contextID = &contextIDString
	}

	taskID := uuid.New().String()
	if request.Message.TaskID != nil && *request.Message.TaskID != "" {
		taskID = *request.Message.TaskID
	}

	innerQueue := &InMemoryEventQueue{events: []protocol.Event{}}
	queue := NewTaskSavingEventQueue(innerQueue, m.taskStore, taskID, *contextID)

	err := m.executor.Execute(ctx, &request, queue, taskID, *contextID)
	if err != nil {
		return nil, err
	}

	var finalMessage *protocol.Message
	for _, event := range innerQueue.events {
		if statusEvent, ok := event.(*protocol.TaskStatusUpdateEvent); ok && statusEvent.Final {
			if statusEvent.Status.Message != nil {
				finalMessage = statusEvent.Status.Message
			}
		}
	}

	return &protocol.MessageResult{
		Result: finalMessage,
	}, nil
}

func (m *ADKTaskManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	ch := make(chan protocol.StreamingMessageEvent)
	innerQueue := &StreamingEventQueue{ch: ch}

	contextID := resolveContextID(&request.Message)
	if contextID == nil || *contextID == "" {
		contextIDString := uuid.New().String()
		contextID = &contextIDString
		log := logr.FromContextOrDiscard(ctx)
		log.Info("No context_id in request; generated new one — events may not match UI session",
			"generatedContextID", *contextID)
	}

	taskID := uuid.New().String()
	if request.Message.TaskID != nil && *request.Message.TaskID != "" {
		taskID = *request.Message.TaskID
	}

	queue := NewTaskSavingEventQueue(innerQueue, m.taskStore, taskID, *contextID)

	go func() {
		defer close(ch)
		err := m.executor.Execute(ctx, &request, queue, taskID, *contextID)
		if err != nil {
			ch <- protocol.StreamingMessageEvent{
				Result: &protocol.TaskStatusUpdateEvent{
					Kind:      "status-update",
					TaskID:    taskID,
					ContextID: *contextID,
					Status: protocol.TaskStatus{
						State: protocol.TaskStateFailed,
						Message: &protocol.Message{
							MessageID: uuid.New().String(),
							Role:      protocol.MessageRoleAgent,
							Parts: []protocol.Part{
								protocol.NewTextPart(err.Error()),
							},
						},
						Timestamp: time.Now().UTC().Format(time.RFC3339),
					},
					Final: true,
				},
			}
		}
	}()

	return ch, nil
}

func (m *ADKTaskManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
	if m.taskStore == nil {
		return nil, fmt.Errorf("task store not available")
	}

	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	task, err := m.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	if task == nil {
		return nil, nil
	}

	return task, nil
}

func (m *ADKTaskManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
	if m.taskStore == nil {
		return nil, fmt.Errorf("task store not available")
	}

	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	task, err := m.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	if task == nil {
		return nil, nil
	}

	if err := m.taskStore.Delete(ctx, taskID); err != nil {
		return nil, fmt.Errorf("failed to delete task: %w", err)
	}

	return task, nil
}

func (m *ADKTaskManager) OnPushNotificationSet(ctx context.Context, params protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	if m.pushNotificationStore == nil {
		return nil, fmt.Errorf("push notification store not available")
	}

	config, err := m.pushNotificationStore.Set(ctx, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to set push notification: %w", err)
	}

	return config, nil
}

func (m *ADKTaskManager) OnPushNotificationGet(ctx context.Context, params protocol.TaskIDParams) (*protocol.TaskPushNotificationConfig, error) {
	if m.pushNotificationStore == nil {
		return nil, fmt.Errorf("push notification store not available")
	}

	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	return nil, fmt.Errorf("config ID extraction from TaskIDParams not yet implemented - may need protocol update")
}

func (m *ADKTaskManager) OnResubscribe(ctx context.Context, params protocol.TaskIDParams) (<-chan protocol.StreamingMessageEvent, error) {
	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	if m.taskStore == nil {
		return nil, fmt.Errorf("task store not available")
	}

	task, err := m.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	if task == nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	contextID := task.ContextID
	if contextID == "" {
		return nil, fmt.Errorf("task has no context ID")
	}

	ch := make(chan protocol.StreamingMessageEvent)

	go func() {
		defer close(ch)

		if task.History != nil {
			for i := range task.History {
				select {
				case ch <- protocol.StreamingMessageEvent{
					Result: &task.History[i],
				}:
				case <-ctx.Done():
					return
				}
			}
		}

		isFinal := task.Status.State == protocol.TaskStateCompleted ||
			task.Status.State == protocol.TaskStateFailed

		select {
		case ch <- protocol.StreamingMessageEvent{
			Result: &protocol.TaskStatusUpdateEvent{
				Kind:      "status-update",
				TaskID:    taskID,
				ContextID: contextID,
				Status:    task.Status,
				Final:     isFinal,
			},
		}:
		case <-ctx.Done():
			return
		}
	}()

	return ch, nil
}

// TaskSavingEventQueue wraps an EventQueue and automatically saves tasks to task store
// after each event is enqueued (matching Python A2A SDK behavior).
type TaskSavingEventQueue struct {
	inner       EventQueue
	taskStore   *taskstore.KAgentTaskStore
	taskID      string
	contextID   string
	currentTask *protocol.Task
}

func NewTaskSavingEventQueue(inner EventQueue, taskStore *taskstore.KAgentTaskStore, taskID, contextID string) *TaskSavingEventQueue {
	return &TaskSavingEventQueue{
		inner:     inner,
		taskStore: taskStore,
		taskID:    taskID,
		contextID: contextID,
	}
}

func (q *TaskSavingEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	if err := q.inner.EnqueueEvent(ctx, event); err != nil {
		return err
	}
	if q.taskStore == nil {
		return nil
	}
	log := logr.FromContextOrDiscard(ctx)
	task := q.loadOrCreateTask(ctx)
	task.ContextID = q.contextID
	applyEventToTask(task, event)
	if err := q.taskStore.Save(ctx, task); err != nil {
		log.Error(err, "Failed to save task after enqueueing event", "taskID", q.taskID, "eventType", fmt.Sprintf("%T", event))
	} else {
		log.V(1).Info("Saved task after enqueueing event", "taskID", q.taskID, "eventType", fmt.Sprintf("%T", event))
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

// InMemoryEventQueue stores events in memory
type InMemoryEventQueue struct {
	events []protocol.Event
}

func (q *InMemoryEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	q.events = append(q.events, event)
	return nil
}

// StreamingEventQueue streams events to a channel
type StreamingEventQueue struct {
	ch chan protocol.StreamingMessageEvent
}

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
	case q.ch <- streamEvent:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
