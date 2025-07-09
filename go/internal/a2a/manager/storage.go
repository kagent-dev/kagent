package manager

import (
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// Storage defines the interface for persisting task manager data
type Storage interface {
	// Message operations
	StoreMessage(message protocol.Message) error
	GetMessage(messageID string) (protocol.Message, error)
	// List messages by context ID, if limit is -1, return all messages
	ListMessagesByContextID(contextID string, limit int) ([]protocol.Message, error)

	// Task operations
	StoreTask(taskID string, task *MemoryCancellableTask) error
	GetTask(taskID string) (*MemoryCancellableTask, error)
	TaskExists(taskID string) bool

	// Push notification operations
	StorePushNotification(taskID string, config protocol.TaskPushNotificationConfig) error
	GetPushNotification(taskID string) (protocol.TaskPushNotificationConfig, error)
}

// StorageOptions contains configuration options for storage implementations
type StorageOptions struct {
	MaxHistoryLength int
}
