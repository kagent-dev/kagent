# Database Models & Sessions

## Session Model
```go
type Session struct {
    ID        string         // Primary key
    Name      *string        // Optional name
    UserID    string         // Primary key (multi-tenant)
    AgentID   *string        // FK to Agent
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt // Soft delete
}
```

## Task Model (represents a run)
```go
type Task struct {
    ID        string
    SessionID string         // FK to Session
    Data      string         // Serialized protocol.Task (JSON)
    ...
}
```

## Event Model (messages within session)
```go
type Event struct {
    ID        string
    SessionID string
    UserID    string
    Data      string         // Serialized protocol.Message (JSON)
    ...
}
```

## Key Interface Methods
- `StoreSession(session *Session) error`
- `ListSessionsForAgent(agentID, userID string) ([]Session, error)`
- `StoreTask(task *protocol.Task) error`
- `ListTasksForSession(sessionID string) ([]*protocol.Task, error)`

## Migration: GORM AutoMigrate
All models registered in `manager.go` Initialize().

## Upsert Pattern
```go
db.Clauses(clause.OnConflict{UpdateAll: true}).Create(model)
```

## For AgentCronJob
- Each cron execution creates a new Session + sends a message via A2A API
- No new DB models needed — sessions/tasks/events reuse existing models
- CRD status stores session ID for reference
