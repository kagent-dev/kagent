## Database query best practices

### Generic Query Functions

Use type-safe generic functions for common CRUD operations:

From `go/internal/database/service.go` (lines 18-75):

```go
// List all records of type T with optional filtering
func list[T any](db *gorm.DB, query *gorm.DB, userID string) ([]T, error)

// Get single record by ID
func get[T any](db *gorm.DB, id uint, userID string) (*T, error)

// Save (create or update) a record
func save[T any](db *gorm.DB, record *T) error

// Delete a record by ID
func delete[T any](db *gorm.DB, id uint, userID string) error
```

### Query Patterns

**1. List with Filtering**

```go
func (s *Service) ListAgents(ctx context.Context, userID, namespace string) ([]Agent, error) {
    query := s.db.WithContext(ctx)

    // Add filters
    if namespace != "" {
        query = query.Where("namespace = ?", namespace)
    }

    return list[Agent](s.db, query, userID)
}
```

**2. Get by Composite Key**

```go
func (s *Service) GetAgent(ctx context.Context, namespace, name, userID string) (*Agent, error) {
    var agent Agent
    err := s.db.WithContext(ctx).
        Where("namespace = ? AND name = ? AND user_id = ?", namespace, name, userID).
        First(&agent).Error

    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, nil  // Return nil for not found, not error
    }
    return &agent, err
}
```

**3. Create with Validation**

```go
func (s *Service) CreateSession(ctx context.Context, session *Session) error {
    // GORM automatically handles ID generation and timestamps
    return s.db.WithContext(ctx).Create(session).Error
}
```

**4. Update Specific Fields**

```go
func (s *Service) UpdateSession(ctx context.Context, id uint, updates map[string]interface{}, userID string) error {
    return s.db.WithContext(ctx).
        Model(&Session{}).
        Where("id = ? AND user_id = ?", id, userID).
        Updates(updates).Error
}
```

**5. Delete with User Isolation**

```go
func (s *Service) DeleteAgent(ctx context.Context, namespace, name, userID string) error {
    result := s.db.WithContext(ctx).
        Where("namespace = ? AND name = ? AND user_id = ?", namespace, name, userID).
        Delete(&Agent{})

    if result.RowsAffected == 0 {
        return ErrNotFound
    }
    return result.Error
}
```

### SQL Injection Prevention

- **Parameterized Queries**: Always use `?` placeholders with GORM
- **Never Concatenate**: Never use string concatenation for SQL

```go
// ✅ CORRECT - Parameterized
db.Where("name = ? AND namespace = ?", name, namespace)

// ❌ WRONG - SQL injection risk
db.Where(fmt.Sprintf("name = '%s'", name))
```

### User Isolation Pattern

**Always filter by UserID** for multi-tenant data:

```go
// Generic list function enforces user isolation
func list[T any](db *gorm.DB, query *gorm.DB, userID string) ([]T, error) {
    var results []T
    // User isolation applied automatically
    err := query.Where("user_id = ?", userID).Find(&results).Error
    return results, err
}
```

### Error Handling

- **Not Found**: Return `nil` (not an error) when record doesn't exist
- **Check Specific Errors**: Use `errors.Is(err, gorm.ErrRecordNotFound)`
- **Context Errors**: Propagate context cancellation errors

```go
if errors.Is(err, gorm.ErrRecordNotFound) {
    return nil, nil  // Not found is not an error
}
if err != nil {
    return nil, fmt.Errorf("database error: %w", err)
}
```

### Context Usage

- **Always Use Context**: Pass `context.Context` to all database operations
- **WithContext**: Use `db.WithContext(ctx)` for request cancellation support
- **Timeout Propagation**: Context timeouts automatically cancel long queries

```go
func (s *Service) GetSomething(ctx context.Context, id uint) (*Thing, error) {
    var thing Thing
    err := s.db.WithContext(ctx).First(&thing, id).Error
    return &thing, err
}
```

### Transaction Pattern

```go
func (s *Service) CreateWithRelated(ctx context.Context, parent *Parent, children []Child) error {
    return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // Create parent
        if err := tx.Create(parent).Error; err != nil {
            return err
        }

        // Create children with parent ID
        for i := range children {
            children[i].ParentID = parent.ID
        }
        if err := tx.Create(&children).Error; err != nil {
            return err
        }

        return nil  // Commit on success
    })
}
```

### Performance Considerations

- **Indexes**: Defined via GORM tags on models (e.g., `gorm:"index"`)
- **Composite Indexes**: Use for common query combinations (namespace + name)
- **JSON Queries**: GORM handles JSON field queries for PostgreSQL and SQLite
- **Select Specific Fields**: Use `.Select()` when you don't need all columns

```go
// Select only needed fields
var names []string
db.Model(&Agent{}).Select("name").Where("namespace = ?", ns).Pluck("name", &names)
```

### Query Builder Pattern

- **Chainable Methods**: Build complex queries incrementally
- **Conditional Filters**: Add WHERE clauses based on parameters

```go
query := s.db.WithContext(ctx).Model(&Task{})

if sessionID != "" {
    query = query.Where("session_id = ?", sessionID)
}

if status != "" {
    query = query.Where("status = ?", status)
}

if afterTimestamp != "" {
    query = query.Where("created_at > ?", afterTimestamp)
}

query = query.Order("created_at DESC").Limit(limit)

var tasks []Task
err := query.Find(&tasks).Error
```

### Pagination

```go
func (s *Service) ListWithPagination(ctx context.Context, page, pageSize int, userID string) ([]Thing, int64, error) {
    var total int64
    var results []Thing

    query := s.db.WithContext(ctx).Where("user_id = ?", userID)

    // Get total count
    if err := query.Model(&Thing{}).Count(&total).Error; err != nil {
        return nil, 0, err
    }

    // Get paginated results
    offset := (page - 1) * pageSize
    err := query.Offset(offset).Limit(pageSize).Find(&results).Error

    return results, total, err
}
```
