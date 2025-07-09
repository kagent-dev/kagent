package manager

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-a2a-go/log"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

const defaultMaxHistoryLength = 100
const defaultCleanupInterval = 30 * time.Second
const defaultConversationTTL = 1 * time.Hour
const defaultTaskSubscriberBufferSize = 10

// // ConversationHistory stores conversation history information
// type ConversationHistory struct {
// 	// MessageIDs is the list of message IDs, ordered by time
// 	MessageIDs []string
// 	// LastAccessTime is the last access time
// 	LastAccessTime time.Time
// }

// MemoryCancellableTask is a task that can be cancelled
type MemoryCancellableTask struct {
	task       protocol.Task
	cancelFunc context.CancelFunc
	ctx        context.Context
}

// NewCancellableTask creates a new cancellable task
func NewCancellableTask(task protocol.Task) *MemoryCancellableTask {
	cancelCtx, cancel := context.WithCancel(context.Background())
	return &MemoryCancellableTask{
		task:       task,
		cancelFunc: cancel,
		ctx:        cancelCtx,
	}
}

// Cancel cancels the task
func (t *MemoryCancellableTask) Cancel() {
	t.cancelFunc()
}

// Task returns the task
func (t *MemoryCancellableTask) Task() *protocol.Task {
	return &t.task
}

// TaskSubscriber is a subscriber for a task
type TaskSubscriber struct {
	taskID         string
	eventQueue     chan protocol.StreamingMessageEvent
	lastAccessTime time.Time
	closed         atomic.Bool
	mu             sync.RWMutex
}

// NewTaskSubscriber creates a new task subscriber with specified buffer length
func NewTaskSubscriber(taskID string, length int) *TaskSubscriber {
	if length <= 0 {
		length = defaultTaskSubscriberBufferSize // default buffer size
	}

	eventQueue := make(chan protocol.StreamingMessageEvent, length)

	return &TaskSubscriber{
		taskID:         taskID,
		eventQueue:     eventQueue,
		lastAccessTime: time.Now(),
		closed:         atomic.Bool{},
	}
}

// Close closes the task subscriber
func (s *TaskSubscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed.Load() {
		s.closed.Store(true)
		close(s.eventQueue)
	}
}

// Channel returns the channel of the task subscriber
func (s *TaskSubscriber) Channel() <-chan protocol.StreamingMessageEvent {
	return s.eventQueue
}

// Closed returns true if the task subscriber is closed
func (s *TaskSubscriber) Closed() bool {
	return s.closed.Load()
}

// Send sends an event to the task subscriber
func (s *TaskSubscriber) Send(event protocol.StreamingMessageEvent) error {
	if s.Closed() {
		return fmt.Errorf("task subscriber is closed")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Closed() {
		return fmt.Errorf("task subscriber is closed")
	}

	s.lastAccessTime = time.Now()

	// Use select with default to avoid blocking
	select {
	case s.eventQueue <- event:
		return nil
	default:
		return fmt.Errorf("event queue is full or closed")
	}
}

// GetLastAccessTime returns the last access time
func (s *TaskSubscriber) GetLastAccessTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastAccessTime
}

// TaskManager is the implementation of the TaskManager interface
type TaskManager struct {
	// mu protects the following fields
	mu sync.RWMutex

	// Processor is the user-provided message Processor
	Processor taskmanager.MessageProcessor

	// Storage handles data persistence
	Storage Storage

	// taskMu protects the Tasks field
	taskMu sync.RWMutex

	// Subscribers stores the task subscribers
	// key: taskID, value: TaskSubscriber list
	// supports all event types: Message, Task, TaskStatusUpdateEvent, TaskArtifactUpdateEvent
	Subscribers map[string][]*TaskSubscriber
}

// NewTaskManager creates a new TaskManager instance
func NewTaskManager(processor taskmanager.MessageProcessor, storage Storage) (*TaskManager, error) {
	if processor == nil {
		return nil, fmt.Errorf("processor cannot be nil")
	}
	if storage == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}

	manager := &TaskManager{
		Processor:   processor,
		Storage:     storage,
		Subscribers: make(map[string][]*TaskSubscriber),
	}

	return manager, nil
}

// =============================================================================
// TaskManager interface implementation
// =============================================================================

// OnSendMessage handles the message/tasks request
func (m *TaskManager) OnSendMessage(
	ctx context.Context,
	request protocol.SendMessageParams,
) (*protocol.MessageResult, error) {
	log.Debugf("TaskManager: OnSendMessage for message %s", request.Message.MessageID)

	// process the request message
	if err := m.processRequestMessage(&request.Message); err != nil {
		return nil, fmt.Errorf("failed to process request message: %w", err)
	}

	// process Configuration
	options := m.processConfiguration(request.Configuration, request.Metadata)
	options.Streaming = false // non-streaming processing

	// create MessageHandle
	handle := &taskHandler{
		manager:   m,
		messageID: request.Message.MessageID,
		ctx:       ctx,
	}

	// call the user's message processor
	result, err := m.Processor.ProcessMessage(ctx, request.Message, options, handle)
	if err != nil {
		return nil, fmt.Errorf("message processing failed: %w", err)
	}

	if result == nil {
		return nil, fmt.Errorf("processor returned nil result")
	}

	// check if the user returned StreamingEvents for non-streaming request
	if result.StreamingEvents != nil {
		log.Infof("User returned StreamingEvents for non-streaming request, ignoring")
	}

	if result.Result == nil {
		return nil, fmt.Errorf("processor returned nil result for non-streaming request")
	}

	switch result.Result.(type) {
	case *protocol.Task:
	case *protocol.Message:
	default:
		return nil, fmt.Errorf("processor returned unsupported result type %T for SendMessage request", result.Result)
	}

	if message, ok := result.Result.(*protocol.Message); ok {
		var contextID string
		if request.Message.ContextID != nil {
			contextID = *request.Message.ContextID
		}
		if err := m.processReplyMessage(contextID, message); err != nil {
			return nil, fmt.Errorf("failed to process reply message: %w", err)
		}
	}

	return &protocol.MessageResult{Result: result.Result}, nil
}

// OnSendMessageStream handles message/stream requests
func (m *TaskManager) OnSendMessageStream(
	ctx context.Context,
	request protocol.SendMessageParams,
) (<-chan protocol.StreamingMessageEvent, error) {
	log.Debugf("TaskManager: OnSendMessageStream for message %s", request.Message.MessageID)

	if err := m.processRequestMessage(&request.Message); err != nil {
		return nil, fmt.Errorf("failed to process request message: %w", err)
	}

	// Process Configuration
	options := m.processConfiguration(request.Configuration, request.Metadata)
	options.Streaming = true // streaming mode

	// Create streaming MessageHandle
	handle := &taskHandler{
		manager:   m,
		messageID: request.Message.MessageID,
		ctx:       ctx,
	}

	// Call user's message processor
	result, err := m.Processor.ProcessMessage(ctx, request.Message, options, handle)
	if err != nil {
		return nil, fmt.Errorf("message processing failed: %w", err)
	}

	if result == nil || result.StreamingEvents == nil {
		return nil, fmt.Errorf("processor returned nil result")
	}

	return result.StreamingEvents.Channel(), nil
}

// OnGetTask handles the tasks/get request
func (m *TaskManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
	task, err := m.Storage.GetTask(params.ID)
	if err != nil {
		return nil, err
	}

	// return a copy of the task
	taskCopy := *task.Task()

	// if the request contains history length, fill the message history
	if params.HistoryLength != nil && *params.HistoryLength > 0 {
		if taskCopy.ContextID != "" {
			history := m.getConversationHistory(taskCopy.ContextID, *params.HistoryLength)
			taskCopy.History = history
		}
	}

	return &taskCopy, nil
}

// OnCancelTask handles the tasks/cancel request
func (m *TaskManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
	task, err := m.Storage.GetTask(params.ID)
	if err != nil {
		return nil, err
	}

	taskCopy := *task.Task()

	handle := &taskHandler{
		manager: m,
		ctx:     ctx,
	}
	handle.CleanTask(&params.ID)
	taskCopy.Status.State = protocol.TaskStateCanceled
	taskCopy.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)

	return &taskCopy, nil
}

// OnPushNotificationSet handles tasks/pushNotificationConfig/set requests
func (m *TaskManager) OnPushNotificationSet(
	ctx context.Context,
	params protocol.TaskPushNotificationConfig,
) (*protocol.TaskPushNotificationConfig, error) {
	err := m.Storage.StorePushNotification(params.TaskID, params)
	if err != nil {
		return nil, fmt.Errorf("failed to store push notification config: %w", err)
	}
	log.Debugf("TaskManager: Push notification config set for task %s", params.TaskID)
	return &params, nil
}

// OnPushNotificationGet handles tasks/pushNotificationConfig/get requests
func (m *TaskManager) OnPushNotificationGet(
	ctx context.Context,
	params protocol.TaskIDParams,
) (*protocol.TaskPushNotificationConfig, error) {
	config, err := m.Storage.GetPushNotification(params.ID)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// OnResubscribe handles tasks/resubscribe requests
func (m *TaskManager) OnResubscribe(
	ctx context.Context,
	params protocol.TaskIDParams,
) (<-chan protocol.StreamingMessageEvent, error) {
	// Check if task exists
	if _, err := m.Storage.GetTask(params.ID); err != nil {
		return nil, fmt.Errorf("task not found: %s", params.ID)
	}

	m.taskMu.Lock()
	defer m.taskMu.Unlock()

	subscriber := NewTaskSubscriber(params.ID, defaultTaskSubscriberBufferSize)

	// Add to subscribers list
	if _, exists := m.Subscribers[params.ID]; !exists {
		m.Subscribers[params.ID] = make([]*TaskSubscriber, 0)
	}
	m.Subscribers[params.ID] = append(m.Subscribers[params.ID], subscriber)

	return subscriber.eventQueue, nil
}

// OnSendTask deprecated method empty implementation
func (m *TaskManager) OnSendTask(ctx context.Context, request protocol.SendTaskParams) (*protocol.Task, error) {
	return nil, fmt.Errorf("OnSendTask is deprecated, use OnSendMessage instead")
}

// OnSendTaskSubscribe deprecated method empty implementation
func (m *TaskManager) OnSendTaskSubscribe(ctx context.Context, request protocol.SendTaskParams) (<-chan protocol.TaskEvent, error) {
	return nil, fmt.Errorf("OnSendTaskSubscribe is deprecated, use OnSendMessageStream instead")
}

// =============================================================================
// Internal helper methods
// =============================================================================

// storeMessage stores messages
func (m *TaskManager) storeMessage(message protocol.Message) error {
	log.Infof("Storing message %s", message.MessageID)
	// Store the message using the storage interface
	return m.Storage.StoreMessage(message)
}

// getMessageHistory gets message history
func (m *TaskManager) getMessageHistory(contextID string) []protocol.Message {
	if contextID == "" {
		return []protocol.Message{}
	}

	messages, err := m.Storage.ListMessagesByContextID(contextID, defaultMaxHistoryLength)
	if err != nil {
		return []protocol.Message{}
	}

	return messages
}

// getConversationHistory gets conversation history of specified length
func (m *TaskManager) getConversationHistory(contextID string, length int) []protocol.Message {
	if contextID == "" {
		return []protocol.Message{}
	}

	messages, err := m.Storage.ListMessagesByContextID(contextID, length)
	if err != nil {
		return []protocol.Message{}
	}

	return messages
}

// isFinalState checks if it's a final state
func isFinalState(state protocol.TaskState) bool {
	return state == protocol.TaskStateCompleted ||
		state == protocol.TaskStateFailed ||
		state == protocol.TaskStateCanceled ||
		state == protocol.TaskStateRejected
}

// =============================================================================
// Configuration related types and helper methods
// =============================================================================

// processConfiguration processes and normalizes Configuration
func (m *TaskManager) processConfiguration(config *protocol.SendMessageConfiguration, metadata map[string]interface{}) taskmanager.ProcessOptions {
	result := taskmanager.ProcessOptions{
		Blocking:      false,
		HistoryLength: 0,
	}

	if config == nil {
		return result
	}

	// Process Blocking configuration
	if config.Blocking != nil {
		result.Blocking = *config.Blocking
	}

	// Process HistoryLength configuration
	if config.HistoryLength != nil && *config.HistoryLength > 0 {
		result.HistoryLength = *config.HistoryLength
	}

	// Process PushNotificationConfig
	if config.PushNotificationConfig != nil {
		result.PushNotificationConfig = config.PushNotificationConfig
	}

	return result
}

func (m *TaskManager) processRequestMessage(message *protocol.Message) error {
	if message.MessageID == "" {
		message.MessageID = protocol.GenerateMessageID()
	}
	return m.storeMessage(*message)
}

func (m *TaskManager) processReplyMessage(ctxID string, message *protocol.Message) error {
	message.ContextID = &ctxID
	message.Role = protocol.MessageRoleAgent
	if message.MessageID == "" {
		message.MessageID = protocol.GenerateMessageID()
	}
	return m.storeMessage(*message)
}

func (m *TaskManager) getTask(taskID string) (*MemoryCancellableTask, error) {
	return m.Storage.GetTask(taskID)
}

// notifySubscribers notifies all subscribers of the task
func (m *TaskManager) notifySubscribers(taskID string, event protocol.StreamingMessageEvent) {
	m.taskMu.RLock()
	subs, exists := m.Subscribers[taskID]
	if !exists || len(subs) == 0 {
		m.taskMu.RUnlock()
		return
	}

	subsCopy := make([]*TaskSubscriber, len(subs))
	copy(subsCopy, subs)
	m.taskMu.RUnlock()

	log.Debugf("Notifying %d subscribers for task %s (Event Type: %T)", len(subsCopy), taskID, event.Result)

	var failedSubscribers []*TaskSubscriber

	for _, sub := range subsCopy {
		if sub.Closed() {
			log.Debugf("Subscriber for task %s is already closed, marking for removal", taskID)
			failedSubscribers = append(failedSubscribers, sub)
			continue
		}

		err := sub.Send(event)
		if err != nil {
			log.Warnf("Failed to send event to subscriber for task %s: %v", taskID, err)
			failedSubscribers = append(failedSubscribers, sub)
		}
	}

	// Clean up failed or closed subscribers
	if len(failedSubscribers) > 0 {
		m.cleanupFailedSubscribers(taskID, failedSubscribers)
	}
}

// cleanupFailedSubscribers cleans up failed or closed subscribers
func (m *TaskManager) cleanupFailedSubscribers(taskID string, failedSubscribers []*TaskSubscriber) {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()

	subs, exists := m.Subscribers[taskID]
	if !exists {
		return
	}

	// Filter out failed subscribers
	filteredSubs := make([]*TaskSubscriber, 0, len(subs))
	removedCount := 0

	for _, sub := range subs {
		shouldRemove := false
		for _, failedSub := range failedSubscribers {
			if sub == failedSub {
				shouldRemove = true
				removedCount++
				break
			}
		}
		if !shouldRemove {
			filteredSubs = append(filteredSubs, sub)
		}
	}

	if removedCount > 0 {
		m.Subscribers[taskID] = filteredSubs
		log.Debugf("Removed %d failed subscribers for task %s", removedCount, taskID)

		// If there are no subscribers left, delete the entire entry
		if len(filteredSubs) == 0 {
			delete(m.Subscribers, taskID)
		}
	}
}

// addSubscriber adds a subscriber
func (m *TaskManager) addSubscriber(taskID string, sub *TaskSubscriber) {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()

	if _, exists := m.Subscribers[taskID]; !exists {
		m.Subscribers[taskID] = make([]*TaskSubscriber, 0)
	}
	m.Subscribers[taskID] = append(m.Subscribers[taskID], sub)
}

// cleanSubscribers cleans up subscribers
func (m *TaskManager) cleanSubscribers(taskID string) {
	m.taskMu.Lock()
	defer m.taskMu.Unlock()
	for _, sub := range m.Subscribers[taskID] {
		sub.Close()
	}
	delete(m.Subscribers, taskID)
}
