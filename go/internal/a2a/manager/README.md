# TaskManager Storage Refactoring

This package contains the refactored TaskManager that uses a Storage interface for persistence instead of in-memory maps.

## Overview

The TaskManager has been refactored to support multiple storage backends through the `Storage` interface. This allows you to choose between in-memory storage for development/testing and persistent database storage for production.

## Storage Interface

The `Storage` interface defines methods for:
- **Messages**: Store, retrieve, and delete protocol messages
- **Conversations**: Manage conversation history and access tracking
- **Tasks**: Store and manage cancellable tasks
- **Push Notifications**: Handle push notification configurations
- **Cleanup**: Manage expired conversation cleanup

## Storage Implementations

### 1. MemoryStorage

An in-memory implementation suitable for:
- Development and testing
- Single-instance deployments
- Scenarios where persistence is not required

```go
storageOpts := DefaultStorageOptions()
manager, err := NewTaskManagerWithMemoryStorage(processor, storageOpts)
```

### 2. GormStorage

A GORM-based implementation that supports:
- SQLite, PostgreSQL, MySQL, and other GORM-supported databases
- Persistent storage across restarts
- Concurrent access from multiple instances
- Proper transaction handling

```go
db, err := gorm.Open(sqlite.Open("taskmanager.db"), &gorm.Config{})
if err != nil {
    return err
}

storageOpts := DefaultStorageOptions()
manager, err := NewTaskManagerWithGormStorage(processor, db, storageOpts)
```

## Key Features

### Automatic Migration
The GormStorage implementation automatically creates the required database tables:
- `a2a_messages`: Stores protocol messages
- `a2a_conversations`: Tracks conversation history and access times
- `a2a_tasks`: Stores task information (simplified, without context.CancelFunc)
- `a2a_push_notifications`: Stores push notification configurations

### History Management
Both storage implementations respect the `MaxHistoryLength` setting to limit conversation history size and automatically clean up old messages.

### Concurrent Access
- MemoryStorage uses read-write mutexes for thread safety
- GormStorage leverages database transactions for consistency

### Error Handling
All storage operations return errors that are properly propagated through the TaskManager methods.

## Configuration Options

### StorageOptions
```go
type StorageOptions struct {
    MaxHistoryLength int  // Maximum number of messages per conversation
}
```

### ManagerOptions
```go
type ManagerOptions struct {
    EnableCleanup   bool          // Enable automatic cleanup of expired conversations
    CleanupInterval time.Duration // How often to run cleanup
    ConversationTTL time.Duration // Time after which conversations expire
}
```

## Usage Examples

See `example.go` for complete examples of using both storage implementations.

## Migration Notes

### From Original Implementation
The original TaskManager used in-memory maps directly. When migrating:

1. Replace `taskmanager.NewMemoryTaskManager()` calls with `NewTaskManagerWithMemoryStorage()`
2. Add storage configuration options
3. Handle storage-related errors in your code

### Database Schema
The GORM implementation stores tasks in a simplified format since `context.CancelFunc` cannot be serialized. When tasks are retrieved, new cancellation contexts are created.

## Performance Considerations

- **MemoryStorage**: Fast read/write operations, but limited by available RAM
- **GormStorage**: Slightly slower due to database I/O, but supports much larger datasets and persistence

## Future Enhancements

Potential improvements could include:
- Redis-based storage implementation
- Distributed storage with consensus mechanisms
- Configurable serialization formats
- Optimized batch operations for high-throughput scenarios 