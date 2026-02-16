package server

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/taskstore"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// A2ATaskManager implements taskmanager.TaskManager using the A2aAgentExecutor.
type A2ATaskManager struct {
	executor              *core.A2aAgentExecutor
	taskStore             *taskstore.KAgentTaskStore
	pushNotificationStore *taskstore.KAgentPushNotificationStore
	logger                logr.Logger
}

// NewA2ATaskManager creates a new A2A task manager.
func NewA2ATaskManager(executor *core.A2aAgentExecutor, taskStore *taskstore.KAgentTaskStore, pushNotificationStore *taskstore.KAgentPushNotificationStore, logger logr.Logger) taskmanager.TaskManager {
	return &A2ATaskManager{
		executor:              executor,
		taskStore:             taskStore,
		pushNotificationStore: pushNotificationStore,
		logger:                logger,
	}
}

// OnSendMessage handles non-streaming message requests.
func (m *A2ATaskManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	contextID := resolveContextID(&request.Message)
	if contextID == nil || *contextID == "" {
		contextIDString := uuid.New().String()
		contextID = &contextIDString
	}

	taskID := uuid.New().String()
	if request.Message.TaskID != nil && *request.Message.TaskID != "" {
		taskID = *request.Message.TaskID
	}

	innerQueue := &InMemoryEventQueue{Events: []protocol.Event{}}
	queue := NewTaskSavingEventQueue(innerQueue, m.taskStore, taskID, *contextID, m.logger)

	err := m.executor.Execute(ctx, &request, queue, taskID, *contextID)
	if err != nil {
		return nil, err
	}

	var finalMessage *protocol.Message
	for _, event := range innerQueue.Events {
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

// OnSendMessageStream handles streaming message requests.
func (m *A2ATaskManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	ch := make(chan protocol.StreamingMessageEvent)
	innerQueue := &StreamingEventQueue{Ch: ch}

	contextID := resolveContextID(&request.Message)
	if contextID == nil || *contextID == "" {
		contextIDString := uuid.New().String()
		contextID = &contextIDString
		if m.logger.GetSink() != nil {
			m.logger.Info("No context_id in request; generated new one — events may not match UI session",
				"generatedContextID", *contextID)
		}
	}

	taskID := uuid.New().String()
	if request.Message.TaskID != nil && *request.Message.TaskID != "" {
		taskID = *request.Message.TaskID
	}

	queue := NewTaskSavingEventQueue(innerQueue, m.taskStore, taskID, *contextID, m.logger)

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

// OnGetTask retrieves a task by ID.
func (m *A2ATaskManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
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

// OnCancelTask cancels a task by ID.
func (m *A2ATaskManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
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

// OnPushNotificationSet sets push notification configuration.
func (m *A2ATaskManager) OnPushNotificationSet(ctx context.Context, params protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	if m.pushNotificationStore == nil {
		return nil, fmt.Errorf("push notification store not available")
	}

	config, err := m.pushNotificationStore.Set(ctx, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to set push notification: %w", err)
	}

	return config, nil
}

// OnPushNotificationGet retrieves push notification configuration.
func (m *A2ATaskManager) OnPushNotificationGet(ctx context.Context, params protocol.TaskIDParams) (*protocol.TaskPushNotificationConfig, error) {
	if m.pushNotificationStore == nil {
		return nil, fmt.Errorf("push notification store not available")
	}

	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	return nil, fmt.Errorf("config ID extraction from TaskIDParams not yet implemented - may need protocol update")
}

// OnResubscribe resubscribes to task events.
func (m *A2ATaskManager) OnResubscribe(ctx context.Context, params protocol.TaskIDParams) (<-chan protocol.StreamingMessageEvent, error) {
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

// resolveContextID returns the session/context ID from the message for event persistence.
func resolveContextID(msg *protocol.Message) *string {
	if msg == nil {
		return nil
	}
	if msg.ContextID != nil && *msg.ContextID != "" {
		return msg.ContextID
	}
	if msg.Metadata != nil {
		for _, key := range []string{a2a.GetKAgentMetadataKey(a2a.MetadataKeySessionID), "contextId", "context_id"} {
			if v, ok := msg.Metadata[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return &s
				}
			}
		}
	}
	return nil
}
