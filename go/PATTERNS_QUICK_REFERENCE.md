# Kagent Go Codebase: Quick Reference Guide

## Quick Navigation

- **Full Analysis**: See `CODEBASE_ANALYSIS.md` for comprehensive details
- **This Guide**: Quick patterns and examples for common tasks

---

## API Patterns

### Adding a New HTTP Handler

1. **Create handler file**: `internal/httpserver/handlers/{resource}.go`
2. **Define handler type**:
   ```go
   type MyResourceHandler struct {
       *Base
   }
   ```
3. **Create constructor**:
   ```go
   func NewMyResourceHandler(base *Base) *MyResourceHandler {
       return &MyResourceHandler{Base: base}
   }
   ```
4. **Implement handler methods**:
   ```go
   func (h *MyResourceHandler) HandleListMyResources(w ErrorResponseWriter, r *http.Request) {
       log := ctrllog.FromContext(r.Context()).WithName("myresource-handler").WithValues("operation", "list")
       
       // 1. Validate request
       userID, err := GetUserID(r)
       if err != nil {
           w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
           return
       }
       log = log.WithValues("userID", userID)
       
       // 2. Execute business logic
       log.V(1).Info("Listing my resources")
       resources, err := h.DatabaseService.ListMyResources(userID)
       if err != nil {
           w.RespondWithError(errors.NewInternalServerError("Failed to list resources", err))
           return
       }
       
       // 3. Return response
       log.Info("Successfully listed resources", "count", len(resources))
       data := api.NewResponse(resources, "Successfully listed resources", false)
       RespondWithJSON(w, http.StatusOK, data)
   }
   ```
5. **Add to handlers.go**:
   ```go
   type Handlers struct {
       // ... existing handlers
       MyResource *MyResourceHandler
   }
   
   func NewHandlers(...) *Handlers {
       // ... create base ...
       return &Handlers{
           // ... other handlers ...
           MyResource: NewMyResourceHandler(base),
       }
   }
   ```
6. **Register routes in server.go**:
   ```go
   s.router.HandleFunc(APIPathMyResources, 
       adaptHandler(s.handlers.MyResource.HandleListMyResources)).Methods(http.MethodGet)
   ```

---

## Database Patterns

### Creating a GORM Model

```go
type MyEntity struct {
    // Primary key (required)
    ID        string         `gorm:"primaryKey" json:"id"`
    
    // Timestamps (automatic)
    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
    
    // User association
    UserID    string         `gorm:"primaryKey" json:"user_id"`
    
    // Fields with indexes
    Status    string         `gorm:"index" json:"status"`
    
    // Complex types as JSON
    Config    *MyConfig      `gorm:"type:json" json:"config"`
    
    // Text fields
    Description string       `gorm:"type:text" json:"description"`
}

// REQUIRED: TableName method
func (MyEntity) TableName() string { return "my_entity" }
```

**Key Points**:
- Always include `ID`, `CreatedAt`, `UpdatedAt`, `DeletedAt`
- Use `gorm:"primaryKey"` for composite keys
- Use `gorm:"index"` for frequently queried fields
- Use `gorm:"type:json"` for complex fields
- Always implement `TableName()` method

### Adding Database Operations

1. **Define interface in client.go**:
   ```go
   type Client interface {
       StoreMyEntity(entity *MyEntity) error
       ListMyEntities(userID string) ([]MyEntity, error)
       GetMyEntity(id string, userID string) (*MyEntity, error)
       DeleteMyEntity(id string) error
   }
   ```

2. **Implement in clientImpl**:
   ```go
   func (c *clientImpl) StoreMyEntity(entity *MyEntity) error {
       return save(c.db, entity)
   }
   
   func (c *clientImpl) ListMyEntities(userID string) ([]MyEntity, error) {
       return list[MyEntity](c.db, Clause{Key: "user_id", Value: userID})
   }
   
   func (c *clientImpl) GetMyEntity(id string, userID string) (*MyEntity, error) {
       return get[MyEntity](c.db,
           Clause{Key: "id", Value: id},
           Clause{Key: "user_id", Value: userID})
   }
   
   func (c *clientImpl) DeleteMyEntity(id string) error {
       return delete[MyEntity](c.db, Clause{Key: "id", Value: id})
   }
   ```

3. **Register in manager.go**:
   ```go
   func (m *Manager) Initialize() error {
       // ... existing code ...
       err := m.db.AutoMigrate(
           // ... existing models ...
           &MyEntity{},
       )
       // ... rest of code ...
   }
   ```

### Writing Complex Queries

**Pattern: Query with optional filters**
```go
func (c *clientImpl) SearchMyEntities(userID string, status *string, limit int) ([]MyEntity, error) {
    query := c.db.Where("user_id = ?", userID)
    
    if status != nil {
        query = query.Where("status = ?", *status)
    }
    
    if limit > 0 {
        query = query.Limit(limit)
    }
    
    var entities []MyEntity
    if err := query.Order("created_at DESC").Find(&entities).Error; err != nil {
        return nil, fmt.Errorf("failed to search entities: %w", err)
    }
    return entities, nil
}
```

**Pattern: Transaction for atomic operations**
```go
func (c *clientImpl) UpdateWithRelated(entity *MyEntity, relatedIds []string) error {
    return c.db.Transaction(func(tx *gorm.DB) error {
        // Step 1
        if err := save(tx, entity); err != nil {
            return fmt.Errorf("failed to save entity: %w", err)
        }
        
        // Step 2
        for _, id := range relatedIds {
            if err := save(tx, &RelatedEntity{EntityID: entity.ID, RelatedID: id}); err != nil {
                return fmt.Errorf("failed to save relation: %w", err)
            }
        }
        
        return nil
    })
}
```

---

## Error Handling Patterns

### Error Types to Use

```go
// Request validation errors
errors.NewBadRequestError("Invalid input", err)

// Resource not found
errors.NewNotFoundError("Resource not found", err)

// Authorization errors
errors.NewForbiddenError("Not authorized", err)

// Business logic conflicts
errors.NewConflictError("Resource already exists", err)

// Validation errors
errors.NewValidationError("Invalid data", err)

// Server errors
errors.NewInternalServerError("Failed to process", err)

// Not implemented
errors.NewNotImplementedError("Feature not available", err)
```

### Proper Error Wrapping

```go
// DO: Wrap errors with context
if err := someOperation(); err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}

// DON'T: Lose error information
if err := someOperation(); err != nil {
    return err  // What failed? Why?
}

// DON'T: Create error strings instead of wrapping
if err := someOperation(); err != nil {
    return fmt.Errorf("error: %v", err)  // Info is there but not chainable
}
```

---

## Naming Conventions

### Quick Reference Table

| Item | Pattern | Example |
|------|---------|---------|
| Constants | `APIPath{Resource}` | `APIPathSessions` |
| Interfaces | PascalCase | `Client`, `Handler` |
| Implementations | lowercase with suffix or embed | `clientImpl` |
| Handler types | `{Resource}Handler` | `SessionsHandler` |
| Handler methods | `Handle{Action}{Resource}` | `HandleListSessions` |
| Database methods | `{Verb}{Noun}` | `StoreSessions` |
| Constructors | `New{Type}` | `NewSessionsHandler` |
| Variables | Short (1-2 chars) | `w`, `r`, `h`, `c` |
| JSON fields | `snake_case` | `created_at` |
| Type names | PascalCase | `Session`, `Agent` |
| Packages | lowercase | `handlers`, `database` |

### Handler Method Naming Examples

```go
HandleListResources    // GET /api/resources
HandleGetResource      // GET /api/resources/{id}
HandleCreateResource   // POST /api/resources
HandleUpdateResource   // PUT /api/resources/{id}
HandleDeleteResource   // DELETE /api/resources/{id}
```

### Database Operation Naming

```go
Store{Entity}     // Create or update
Get{Entity}       // Retrieve single
List{Entity}s     // Retrieve multiple
Delete{Entity}    // Delete
Search{Entity}s   // Search with filters
```

---

## Middleware Patterns

### Creating Custom Middleware

```go
func myMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Before handler
        log := ctrllog.Log.WithName("middleware").WithValues("path", r.URL.Path)
        log.V(1).Info("Middleware processing")
        
        // Call next handler
        next.ServeHTTP(w, r)
        
        // After handler (if applicable)
        log.V(1).Info("Middleware complete")
    })
}

// Register middleware
s.router.Use(myMiddleware)
```

---

## Testing Patterns

### Mock Interface

```go
type MockClient struct {
    mock.Mock
}

func (m *MockClient) StoreSession(session *Session) error {
    args := m.Called(session)
    return args.Error(0)
}

// Usage in test
mockClient := new(MockClient)
mockClient.On("StoreSession", mock.MatchedBy(func(s *Session) bool {
    return s.UserID == "test-user"
})).Return(nil)

// Assert mock was called
mockClient.AssertCalled(t, "StoreSession", mock.Anything)
```

---

## File Organization Template

For new features, create these files in order:

1. **Model**: `internal/database/models.go`
   - Add struct type
   - Add TableName() method
   - Update manager.go AutoMigrate()

2. **Database Operations**: `internal/database/client.go`
   - Add interface methods to Client
   - Implement in clientImpl
   - Use helper functions: list[T], get[T], save[T], delete[T]

3. **API Handler**: `internal/httpserver/handlers/{resource}.go`
   - Create {Resource}Handler type
   - Implement Handle* methods
   - Follow standard 4-step flow

4. **Register Handler**: `internal/httpserver/handlers/handlers.go`
   - Add to Handlers struct
   - Initialize in NewHandlers()

5. **Routes**: `internal/httpserver/server.go`
   - Add APIPath constant
   - Register routes in setupRoutes()
   - Apply middleware as needed

---

## Common Imports

```go
// Standard library
import (
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

// Kubernetes
import (
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// Kagent internal
import (
    "github.com/kagent-dev/kagent/go/api/v1alpha2"
    "github.com/kagent-dev/kagent/go/internal/database"
    "github.com/kagent-dev/kagent/go/internal/httpserver/errors"
    "github.com/kagent-dev/kagent/go/pkg/client/api"
)

// GORM
import (
    "gorm.io/gorm"
)
```

---

## Logging Best Practices

```go
// Get logger from context (always)
log := ctrllog.FromContext(r.Context())

// Add names for tracing
log = log.WithName("handler-name")

// Add operation context
log = log.WithValues("operation", "list-items")

// Add contextual data as it emerges
if userID != "" {
    log = log.WithValues("userID", userID)
}

// Use appropriate levels
log.V(1).Info("Verbose debug info")        // Verbose level
log.Info("Important information")           // Default info
log.Error(err, "Something failed")          // Errors with context
```

---

## Key Files to Know

| Functionality | File |
|---------------|------|
| HTTP Server Setup | `internal/httpserver/server.go` |
| Route Registration | `internal/httpserver/server.go:setupRoutes()` |
| Error Types | `internal/httpserver/errors/errors.go` |
| Handler Base | `internal/httpserver/handlers/handlers.go` |
| Database Connection | `internal/database/manager.go` |
| Query Helpers | `internal/database/service.go` |
| Database Operations | `internal/database/client.go` |
| API Response Types | `pkg/client/api/types.go` |
| Auth Middleware | `internal/httpserver/auth/authn.go` |

---

## Useful Commands

```bash
# Format code
go fmt ./...

# Lint code
golangci-lint run ./...

# Run tests
go test ./... -v

# Run specific test
go test -run TestName ./...

# Build
go build -o controller ./cmd/controller

# Database migration (programmatic)
# Handled by manager.Initialize() on startup
```

