## Database model best practices

### Model Structure (GORM)

- **ORM**: Use GORM for database interactions (`gorm.io/gorm`)
- **Database Support**: Support both SQLite (development) and PostgreSQL (production)
- **Model Location**: Define models in `go/internal/database/models.go`
- **Base Fields**: Include common fields using `gorm.Model` (ID, CreatedAt, UpdatedAt, DeletedAt)

Example from `go/internal/database/models.go` (lines 15-24):
```go
type Agent struct {
    gorm.Model
    Name        string `gorm:"not null"`
    Namespace   string `gorm:"not null;index:idx_agent_namespace_name,unique"`
    Spec        datatypes.JSON
    Status      datatypes.JSON
    UserID      string `gorm:"index"`
}
```

### Table Naming Conventions

- **Explicit TableName()**: Always define explicit table names using `TableName()` method for cross-language compatibility
- **Lowercase with Underscores**: Use lowercase with underscores (e.g., `agents`, `model_configs`, `remote_mcp_servers`)
- **Plural Names**: Use plural nouns for table names

Example from `go/internal/database/models.go` (lines 26-28):
```go
func (Agent) TableName() string {
    return "agents"
}
```

### Field Definitions

- **GORM Tags**: Use struct tags for database constraints and indexes
- **NOT NULL**: Mark required fields with `gorm:"not null"`
- **Indexes**: Add indexes on frequently queried fields using `gorm:"index"`
- **Unique Constraints**: Use composite unique indexes where needed (e.g., `index:idx_agent_namespace_name,unique`)
- **JSON Fields**: Use `datatypes.JSON` for flexible/nested data (Spec, Status, metadata)
- **Foreign Keys**: Not heavily used; prefer denormalized design with namespace/name references

### Common Field Patterns

```go
type Model struct {
    gorm.Model              // ID, CreatedAt, UpdatedAt, DeletedAt
    Name      string        `gorm:"not null"`
    Namespace string        `gorm:"not null;index"`
    UserID    string        `gorm:"index"`  // For user isolation
    Spec      datatypes.JSON                // Flexible configuration
    Status    datatypes.JSON                // Runtime state
}
```

### Data Types

- **String**: General text fields (names, descriptions, IDs)
- **datatypes.JSON**: Complex nested structures, configurations
- **time.Time**: Timestamps (automatically handled by gorm.Model)
- **int64/uint**: Numeric IDs and counts
- **bool**: Flags and toggles

### User Isolation

- **UserID Field**: Include `UserID string` with index on all user-scoped resources
- **Query Filtering**: Always filter by UserID in queries for multi-tenant isolation
- **Index on UserID**: Add `gorm:"index"` to UserID fields for query performance

### Migrations

- **Auto-Migration**: Use GORM's AutoMigrate with mutex protection
- **Migration Order**: Migrate models in dependency order
- **Schema Updates**: AutoMigrate handles adding columns and indexes automatically
- **No Down Migrations**: GORM AutoMigrate doesn't support rollbacks

Example from `go/internal/database/service.go` (lines 112-125):
```go
var migrationMutex sync.Mutex

func (s *Service) MigrateModels() error {
    migrationMutex.Lock()
    defer migrationMutex.Unlock()

    return s.db.AutoMigrate(
        &Agent{},
        &Session{},
        &ModelConfig{},
        &RemoteMCPServer{},
        // ... other models
    )
}
```

### Model Organization

- **Single File**: All models in `models.go` for easy reference
- **Logical Grouping**: Group related models (Agents, Sessions, Tasks, Checkpoints, etc.)
- **TableName Methods**: Place TableName() methods immediately after struct definition

### Validation

- **Kubernetes Validation**: Agent names follow RFC 1123 DNS subdomain rules
- **Required Fields**: Use `not null` constraints at database level
- **Application-Level**: Additional validation in handler/service layer before database calls

### JSON Field Usage

Use `datatypes.JSON` for:
- Agent specifications (tools, model config, system prompt)
- Runtime status information
- Checkpoint metadata
- Task context and results
- Flexible configuration that may evolve
