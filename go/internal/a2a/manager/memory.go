package manager

import (
	"fmt"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// MemoryStorage is an in-memory implementation of the Storage interface

type ConversationHistory struct {
	MessageIDs     []string
	LastAccessTime time.Time
}

type MemoryStorage struct {
	mu                sync.RWMutex
	messages          map[string]protocol.Message
	conversations     map[string]*ConversationHistory
	tasks             map[string]*MemoryCancellableTask
	pushNotifications map[string]protocol.TaskPushNotificationConfig
	maxHistoryLength  int
}

// NewMemoryStorage creates a new in-memory storage implementation
func NewMemoryStorage(options StorageOptions) *MemoryStorage {
	maxHistoryLength := options.MaxHistoryLength
	if maxHistoryLength <= 0 {
		maxHistoryLength = defaultMaxHistoryLength
	}

	return &MemoryStorage{
		messages:          make(map[string]protocol.Message),
		conversations:     make(map[string]*ConversationHistory),
		tasks:             make(map[string]*MemoryCancellableTask),
		pushNotifications: make(map[string]protocol.TaskPushNotificationConfig),
		maxHistoryLength:  maxHistoryLength,
	}
}

// Message operations
func (s *MemoryStorage) StoreMessage(message protocol.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages[message.MessageID] = message

	// If the message has a contextID, add it to conversation history
	if message.ContextID != nil {
		contextID := *message.ContextID
		if _, exists := s.conversations[contextID]; !exists {
			s.conversations[contextID] = &ConversationHistory{
				MessageIDs:     make([]string, 0),
				LastAccessTime: time.Now(),
			}
		}

		// Add message ID to conversation history
		s.conversations[contextID].MessageIDs = append(s.conversations[contextID].MessageIDs, message.MessageID)
		// Update last access time
		s.conversations[contextID].LastAccessTime = time.Now()

		// Limit history length
		if len(s.conversations[contextID].MessageIDs) > s.maxHistoryLength {
			// Remove the oldest message
			removedMsgID := s.conversations[contextID].MessageIDs[0]
			s.conversations[contextID].MessageIDs = s.conversations[contextID].MessageIDs[1:]
			// Delete old message from message storage
			delete(s.messages, removedMsgID)
		}
	}

	return nil
}

func (s *MemoryStorage) GetMessage(messageID string) (protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	message, exists := s.messages[messageID]
	if !exists {
		return protocol.Message{}, fmt.Errorf("message not found: %s", messageID)
	}
	return message, nil
}

func (s *MemoryStorage) DeleteMessage(messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.messages, messageID)
	return nil
}

func (s *MemoryStorage) GetMessages(messageIDs []string) ([]protocol.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messages := make([]protocol.Message, 0, len(messageIDs))
	for _, msgID := range messageIDs {
		if msg, exists := s.messages[msgID]; exists {
			messages = append(messages, msg)
		}
	}
	return messages, nil
}

// Conversation operations
func (s *MemoryStorage) StoreConversation(contextID string, history *ConversationHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.conversations[contextID] = history
	return nil
}

func (s *MemoryStorage) GetConversation(contextID string) (*ConversationHistory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conversation, exists := s.conversations[contextID]
	if !exists {
		return nil, fmt.Errorf("conversation not found: %s", contextID)
	}
	return conversation, nil
}

func (s *MemoryStorage) UpdateConversationAccess(contextID string, timestamp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if conversation, exists := s.conversations[contextID]; exists {
		conversation.LastAccessTime = timestamp
	}
	return nil
}

func (s *MemoryStorage) DeleteConversation(contextID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.conversations, contextID)
	return nil
}

func (s *MemoryStorage) GetExpiredConversations(maxAge time.Duration) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	expiredContexts := make([]string, 0)

	for contextID, conversation := range s.conversations {
		if now.Sub(conversation.LastAccessTime) > maxAge {
			expiredContexts = append(expiredContexts, contextID)
		}
	}

	return expiredContexts, nil
}

func (s *MemoryStorage) GetConversationStats() (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalConversations := len(s.conversations)
	totalMessages := len(s.messages)

	oldestAccess := time.Now()
	newestAccess := time.Time{}

	for _, conversation := range s.conversations {
		if conversation.LastAccessTime.Before(oldestAccess) {
			oldestAccess = conversation.LastAccessTime
		}
		if conversation.LastAccessTime.After(newestAccess) {
			newestAccess = conversation.LastAccessTime
		}
	}

	stats := map[string]interface{}{
		"total_conversations": totalConversations,
		"total_messages":      totalMessages,
	}

	if totalConversations > 0 {
		stats["oldest_access"] = oldestAccess
		stats["newest_access"] = newestAccess
	}

	return stats, nil
}

// Task operations
func (s *MemoryStorage) StoreTask(taskID string, task *MemoryCancellableTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks[taskID] = task
	return nil
}

func (s *MemoryStorage) GetTask(taskID string) (*MemoryCancellableTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	return task, nil
}

func (s *MemoryStorage) DeleteTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tasks, taskID)
	return nil
}

func (s *MemoryStorage) TaskExists(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.tasks[taskID]
	return exists
}

// Push notification operations
func (s *MemoryStorage) StorePushNotification(taskID string, config protocol.TaskPushNotificationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pushNotifications[taskID] = config
	return nil
}

func (s *MemoryStorage) GetPushNotification(taskID string) (protocol.TaskPushNotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config, exists := s.pushNotifications[taskID]
	if !exists {
		return protocol.TaskPushNotificationConfig{}, fmt.Errorf("push notification config not found for task: %s", taskID)
	}
	return config, nil
}

func (s *MemoryStorage) DeletePushNotification(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pushNotifications, taskID)
	return nil
}

// Cleanup operations
func (s *MemoryStorage) CleanupExpiredConversations(maxAge time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	expiredContexts := make([]string, 0)
	expiredMessageIDs := make([]string, 0)

	// Find expired conversations
	for contextID, conversation := range s.conversations {
		if now.Sub(conversation.LastAccessTime) > maxAge {
			expiredContexts = append(expiredContexts, contextID)
			expiredMessageIDs = append(expiredMessageIDs, conversation.MessageIDs...)
		}
	}

	// Delete expired conversations
	for _, contextID := range expiredContexts {
		delete(s.conversations, contextID)
	}

	// Delete messages from expired conversations
	for _, messageID := range expiredMessageIDs {
		delete(s.messages, messageID)
	}

	return len(expiredContexts), nil
}
