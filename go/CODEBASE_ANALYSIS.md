# Kagent Go Codebase: Patterns and Conventions Analysis

## Executive Summary

The Kagent Go codebase demonstrates mature software engineering practices with clear architectural patterns, consistent naming conventions, and well-organized code structure. The codebase follows Kubernetes operator patterns, uses GORM for database access, and implements clean HTTP API handlers with comprehensive error handling.

---

## 1. API ENDPOINT DESIGN AND STRUCTURE

### 1.1 HTTP Server Architecture

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/server.go`

The HTTP server follows a clean, modular architecture:

```go
// Lines 21-41: API path constants using descriptive names
const (
    APIPathHealth          = "/health"
    APIPathVersion         = "/version"
    APIPathModelConfig     = "/api/modelconfigs"
    APIPathRuns            = "/api/runs"
    APIPathSessions        = "/api/sessions"
    APIPathAgents          = "/api/agents"
    // ... more endpoints
)

// Lines 49-58: ServerConfig - dependency injection pattern
type ServerConfig struct {
    Router            *mux.Router
    BindAddr          string
    KubeClient        ctrl_client.Client
    A2AHandler        a2a.A2AHandlerMux
    WatchedNamespaces []string
    DbClient          database.Client
    Authenticator     auth.AuthProvider
    Authorizer        auth.Authorizer
}

// Lines 71-79: NewHTTPServer - factory pattern with error handling
func NewHTTPServer(config ServerConfig) (*HTTPServer, error) {
    return &HTTPServer{
        config:        config,
        router:        config.Router,
        handlers:      handlers.NewHandlers(...),
        authenticator: config.Authenticator,
    }, nil
}
```

**Key Patterns**:
- Gorilla Mux router for HTTP routing
- Dependency injection via ServerConfig
- Clear separation of concerns

### 1.2 Route Registration Pattern

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/server.go` (Lines 136-232)

Routes are systematically organized by resource type:

```go
// Resource-based grouping pattern (e.g., Sessions)
s.router.HandleFunc(APIPathSessions, 
    adaptHandler(s.handlers.Sessions.HandleListSessions)).Methods(http.MethodGet)
s.router.HandleFunc(APIPathSessions, 
    adaptHandler(s.handlers.Sessions.HandleCreateSession)).Methods(http.MethodPost)
s.router.HandleFunc(APIPathSessions+"/{session_id}", 
    adaptHandler(s.handlers.Sessions.HandleGetSession)).Methods(http.MethodGet)
s.router.HandleFunc(APIPathSessions+"/{session_id}", 
    adaptHandler(s.handlers.Sessions.HandleUpdateSession)).Methods(http.MethodPut)
s.router.HandleFunc(APIPathSessions+"/{session_id}", 
    adaptHandler(s.handlers.Sessions.HandleDeleteSession)).Methods(http.MethodDelete)

// Middleware stack (lines 228-231)
s.router.Use(auth.AuthnMiddleware(s.authenticator))
s.router.Use(contentTypeMiddleware)
s.router.Use(loggingMiddleware)
s.router.Use(errorHandlerMiddleware)
```

**Naming Convention**:
- `APIPath{Resource}` for endpoint constants
- `Handle{Action}{Resource}` for handler methods
- Path patterns: `/api/{resource}`, `/api/{resource}/{id}`, `/api/{resource}/{namespace}/{name}`

### 1.3 Handler Composition Pattern

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/handlers/handlers.go`

```go
// Lines 11-28: Handlers struct - composition of specialized handlers
type Handlers struct {
    Health          *HealthHandler
    ModelConfig     *ModelConfigHandler
    Model           *ModelHandler
    Provider        *ProviderHandler
    Sessions        *SessionsHandler
    Agents          *AgentsHandler
    Tools           *ToolsHandler
    ToolServers     *ToolServersHandler
    // ... more handlers
}

// Lines 30-36: Base struct - shared dependencies
type Base struct {
    KubeClient         client.Client
    DefaultModelConfig types.NamespacedName
    DatabaseService    database.Client
    Authorizer         auth.Authorizer
}

// Lines 38-64: NewHandlers factory - dependency injection
func NewHandlers(kubeClient client.Client, defaultModelConfig types.NamespacedName, 
    dbService database.Client, watchedNamespaces []string, authorizer auth.Authorizer) *Handlers {
    base := &Base{
        KubeClient:         kubeClient,
        DefaultModelConfig: defaultModelConfig,
        DatabaseService:    dbService,
        Authorizer:         authorizer,
    }
    return &Handlers{
        Health:          NewHealthHandler(),
        ModelConfig:     NewModelConfigHandler(base),
        Sessions:        NewSessionsHandler(base),
        // ... initialize all handlers
    }
}
```

**Pattern Benefits**:
- Single Base struct embedded in each handler type
- Factory function for centralized initialization
- DRY principle - shared dependencies in Base

### 1.4 Handler Implementation Pattern

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/handlers/sessions.go` (Lines 1-150)

```go
// Lines 17-25: Handler type with embedded Base
type SessionsHandler struct {
    *Base
}

func NewSessionsHandler(base *Base) *SessionsHandler {
    return &SessionsHandler{Base: base}
}

// Lines 75-95: Handler method pattern - consistent structure
func (h *SessionsHandler) HandleListSessions(w ErrorResponseWriter, r *http.Request) {
    // 1. Setup logging context
    log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-db")
    
    // 2. Extract and validate request parameters
    userID, err := GetUserID(r)
    if err != nil {
        w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
        return
    }
    log = log.WithValues("userID", userID)
    
    // 3. Execute business logic
    log.V(1).Info("Listing sessions from database")
    sessions, err := h.DatabaseService.ListSessions(userID)
    if err != nil {
        w.RespondWithError(errors.NewInternalServerError("Failed to list sessions", err))
        return
    }
    
    // 4. Format and return response
    log.Info("Successfully listed sessions", "count", len(sessions))
    data := api.NewResponse(sessions, "Successfully listed sessions", false)
    RespondWithJSON(w, http.StatusOK, data)
}
```

**Standard Handler Flow**:
1. Setup logging with context and operation name
2. Validate and extract request parameters
3. Execute business logic with database calls
4. Handle errors with appropriate APIError types
5. Format response and return with status code

---

## 2. DATABASE MODELS AND MIGRATIONS

### 2.1 GORM Model Definition

**File**: `/Users/cwalker/repos/kagent/go/internal/database/models.go`

Models use GORM tags for database configuration:

```go
// Lines 13-24: Agent model with GORM tags
type Agent struct {
    ID        string         `gorm:"primaryKey" json:"id"`
    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
    
    Type string `gorm:"not null" json:"type"`
    // Config is optional and may be nil for some agent types
    Config *adk.AgentConfig `gorm:"type:json" json:"config"`
}

// Lines 58-67: Session model with composite primary key
type Session struct {
    ID        string         `gorm:"primaryKey;not null" json:"id"`
    Name      *string        `gorm:"index" json:"name,omitempty"`
    UserID    string         `gorm:"primaryKey" json:"user_id"`
    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
    
    AgentID *string `gorm:"index" json:"agent_id"`
}

// Lines 151-164: LangGraphCheckpoint model
type LangGraphCheckpoint struct {
    UserID             string         `gorm:"primaryKey;not null" json:"user_id"`
    ThreadID           string         `gorm:"primaryKey;not null" json:"thread_id"`
    CheckpointNS       string         `gorm:"primaryKey;not null;default:''" json:"checkpoint_ns"`
    CheckpointID       string         `gorm:"primaryKey;not null" json:"checkpoint_id"`
    ParentCheckpointID *string        `gorm:"index" json:"parent_checkpoint_id,omitempty"`
    CreatedAt          time.Time      `gorm:"autoCreateTime;index:idx_lgcp_list" json:"created_at"`
    UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt          gorm.DeletedAt `gorm:"index" json:"deleted_at"`
    Metadata           string         `gorm:"type:text;not null" json:"metadata"`
    Checkpoint         string         `gorm:"type:text;not null" json:"checkpoint"`
    CheckpointType     string         `gorm:"not null" json:"checkpoint_type"`
    Version            int            `gorm:"default:1" json:"version"`
}
```

**GORM Tag Patterns**:
- `primaryKey`: Mark primary key field(s)
- `autoCreateTime`: Automatically set creation timestamp
- `autoUpdateTime`: Automatically set update timestamp
- `gorm:"index"`: Create index on field
- `gorm:"type:json"`: Store complex types as JSON
- `gorm:"type:text"`: Store strings as TEXT
- `gorm:"constraint:OnDelete:CASCADE"`: Referential integrity
- Custom indexes: `index:idx_lgcp_list`

### 2.2 TableName Methods - Explicit Naming

**File**: `/Users/cwalker/repos/kagent/go/internal/database/models.go` (Lines 205-217)

```go
// Explicit table name mappings to match Python codebase
func (Agent) TableName() string                    { return "agent" }
func (Event) TableName() string                    { return "event" }
func (Session) TableName() string                  { return "session" }
func (Task) TableName() string                     { return "task" }
func (PushNotification) TableName() string         { return "push_notification" }
func (Feedback) TableName() string                 { return "feedback" }
func (Tool) TableName() string                     { return "tool" }
func (ToolServer) TableName() string               { return "toolserver" }
func (LangGraphCheckpoint) TableName() string      { return "lg_checkpoint" }
func (LangGraphCheckpointWrite) TableName() string { return "lg_checkpoint_write" }
func (CrewAIAgentMemory) TableName() string        { return "crewai_agent_memory" }
func (CrewAIFlowState) TableName() string          { return "crewai_flow_state" }
```

**Pattern**: Ensures exact table name mapping, especially for Python-Go interoperability.

### 2.3 Database Manager - Lifecycle Management

**File**: `/Users/cwalker/repos/kagent/go/internal/database/manager.go`

```go
// Lines 14-39: Manager and Config types
type Manager struct {
    db       *gorm.DB
    initLock sync.Mutex
}

type DatabaseType string
const (
    DatabaseTypeSqlite   DatabaseType = "sqlite"
    DatabaseTypePostgres DatabaseType = "postgres"
)

type Config struct {
    DatabaseType   DatabaseType
    SqliteConfig   *SqliteConfig
    PostgresConfig *PostgresConfig
}

// Lines 46-84: NewManager - factory pattern with error wrapping
func NewManager(config *Config) (*Manager, error) {
    var db *gorm.DB
    var err error
    
    logLevel := logger.Silent
    if val, ok := os.LookupEnv(gormLogLevel); ok {
        // ... parse log level
    }
    
    switch config.DatabaseType {
    case DatabaseTypeSqlite:
        db, err = gorm.Open(sqlite.Open(config.SqliteConfig.DatabasePath), &gorm.Config{
            Logger:         logger.Default.LogMode(logLevel),
            TranslateError: true,
        })
    case DatabaseTypePostgres:
        db, err = gorm.Open(postgres.Open(config.PostgresConfig.URL), &gorm.Config{
            Logger:         logger.Default.LogMode(logLevel),
            TranslateError: true,
        })
    default:
        return nil, fmt.Errorf("invalid database type: %s", config.DatabaseType)
    }
    
    if err != nil {
        return nil, fmt.Errorf("failed to connect to database: %w", err)
    }
    return &Manager{db: db}, nil
}

// Lines 87-114: Initialize - auto-migration with lock
func (m *Manager) Initialize() error {
    if !m.initLock.TryLock() {
        return fmt.Errorf("database initialization already in progress")
    }
    defer m.initLock.Unlock()
    
    err := m.db.AutoMigrate(
        &Agent{},
        &Session{},
        &Task{},
        // ... all model types
    )
    
    if err != nil {
        return fmt.Errorf("failed to migrate database: %w", err)
    }
    return nil
}

// Lines 151-161: Close - proper resource cleanup
func (m *Manager) Close() error {
    if m.db == nil {
        return nil
    }
    sqlDB, err := m.db.DB()
    if err != nil {
        return err
    }
    return sqlDB.Close()
}
```

**Patterns**:
- Multi-database support via factory pattern
- Error wrapping for debugging: `fmt.Errorf("context: %w", err)`
- Mutex protection for concurrent initialization
- Automatic migration on startup
- Graceful resource cleanup

---

## 3. DATABASE QUERY PATTERNS

### 3.1 Generic Query Helpers - Type-Safe Generics

**File**: `/Users/cwalker/repos/kagent/go/internal/database/service.go`

```go
// Lines 13-16: Clause struct - parameterized queries
type Clause struct {
    Key   string
    Value interface{}
}

// Lines 18-31: list[T] - generic listing with filtering
func list[T Model](db *gorm.DB, clauses ...Clause) ([]T, error) {
    var models []T
    query := db
    
    for _, clause := range clauses {
        // SQL injection safe - uses parameterized queries
        query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
    }
    
    err := query.Order("created_at ASC").Find(&models).Error
    if err != nil {
        return nil, fmt.Errorf("failed to list models: %w", err)
    }
    return models, nil
}

// Lines 33-46: get[T] - single item retrieval
func get[T Model](db *gorm.DB, clauses ...Clause) (*T, error) {
    var model T
    query := db
    
    for _, clause := range clauses {
        query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
    }
    
    err := query.First(&model).Error
    if err != nil {
        return nil, fmt.Errorf("failed to get model: %w", err)
    }
    return &model, nil
}

// Lines 52-60: save[T] - upsert pattern
func save[T Model](db *gorm.DB, model *T) error {
    if err := db.Create(model).Error; err != nil {
        if err == gorm.ErrDuplicatedKey {
            return db.Save(model).Error  // Update on duplicate key
        }
        return fmt.Errorf("failed to create model: %w", err)
    }
    return nil
}

// Lines 62-75: delete[T] - parameterized deletion
func delete[T Model](db *gorm.DB, clauses ...Clause) error {
    t := new(T)
    query := db
    
    for _, clause := range clauses {
        query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
    }
    
    result := query.Delete(t)
    if result.Error != nil {
        return fmt.Errorf("failed to delete model: %w", result.Error)
    }
    return nil
}

// Lines 9-11: Model interface
type Model interface {
    TableName() string
}
```

**Generics Pattern Benefits**:
- Type-safe database operations
- Parameterized queries prevent SQL injection
- Consistent error handling
- Minimal code duplication
- Go 1.18+ generics

### 3.2 Complex Query Examples

**File**: `/Users/cwalker/repos/kagent/go/internal/database/client.go`

**Example 1: ListEventsForSession with time-based filtering**

```go
// Lines 336-362: Query with optional filtering
func (c *clientImpl) ListEventsForSession(sessionID, userID string, options QueryOptions) ([]*Event, error) {
    var events []Event
    query := c.db.
        Where("session_id = ?", sessionID).
        Where("user_id = ?", userID).
        Order("created_at DESC")
    
    if !options.After.IsZero() {
        query = query.Where("created_at > ?", options.After)
    }
    
    if options.Limit > 1 {
        query = query.Limit(options.Limit)
    }
    
    err := query.Find(&events).Error
    if err != nil {
        return nil, err
    }
    
    protocolEvents := make([]*Event, 0, len(events))
    for _, event := range events {
        protocolEvents = append(protocolEvents, &event)
    }
    
    return protocolEvents, nil
}
```

**Example 2: Checkpoint queries with JSON filtering**

```go
// Lines 526-570: ListCheckpoints with transaction
func (c *clientImpl) ListCheckpoints(userID, threadID, checkpointNS string, 
    checkpointID *string, limit int) ([]*LangGraphCheckpointTuple, error) {
    
    var checkpointTuples []*LangGraphCheckpointTuple
    if err := c.db.Transaction(func(tx *gorm.DB) error {
        query := c.db.Where(
            "user_id = ? AND thread_id = ? AND checkpoint_ns = ?",
            userID, threadID, checkpointNS,
        )
        
        if checkpointID != nil {
            query = query.Where("checkpoint_id = ?", *checkpointID)
        } else {
            query = query.Order("checkpoint_id DESC")
        }
        
        if limit > 0 {
            query = query.Limit(limit)
        }
        
        var checkpoints []LangGraphCheckpoint
        err := query.Find(&checkpoints).Error
        if err != nil {
            return fmt.Errorf("failed to list checkpoints: %w", err)
        }
        
        // Join with writes table
        for _, checkpoint := range checkpoints {
            var writes []*LangGraphCheckpointWrite
            if err := tx.Where(
                "user_id = ? AND thread_id = ? AND checkpoint_ns = ? AND checkpoint_id = ?",
                userID, threadID, checkpointNS, checkpoint.CheckpointID,
            ).Order("task_id, write_idx").Find(&writes).Error; err != nil {
                return fmt.Errorf("failed to get checkpoint writes: %w", err)
            }
            checkpointTuples = append(checkpointTuples, &LangGraphCheckpointTuple{
                Checkpoint: &checkpoint,
                Writes:     writes,
            })
        }
        return nil
    }); err != nil {
        return nil, fmt.Errorf("failed to list checkpoints: %w", err)
    }
    return checkpointTuples, nil
}
```

**Example 3: JSON search with database-agnostic fallback**

```go
// Lines 602-624: SearchCrewAIMemoryByTask with fallback pattern
func (c *clientImpl) SearchCrewAIMemoryByTask(userID, threadID, taskDescription string, 
    limit int) ([]*CrewAIAgentMemory, error) {
    var memories []*CrewAIAgentMemory
    
    // Fallback pattern: LIKE for SQLite compatibility, JSON_EXTRACT for PostgreSQL
    query := c.db.Where(
        "user_id = ? AND thread_id = ? AND (memory_data LIKE ? OR JSON_EXTRACT(memory_data, '$.task_description') LIKE ?)",
        userID, threadID, "%"+taskDescription+"%", "%"+taskDescription+"%",
    ).Order("created_at DESC, JSON_EXTRACT(memory_data, '$.score') ASC")
    
    if limit > 0 {
        query = query.Limit(limit)
    }
    
    err := query.Find(&memories).Error
    if err != nil {
        return nil, fmt.Errorf("failed to search CrewAI agent memory by task: %w", err)
    }
    
    return memories, nil
}
```

**Patterns**:
- Query builder pattern for flexible WHERE clauses
- Transaction wrapping for atomic operations
- Optional filtering (time ranges, limits)
- Database portability (SQLite + PostgreSQL)
- Error wrapping with context

### 3.3 RefreshToolsForServer - Complex Business Logic

**File**: `/Users/cwalker/repos/kagent/go/internal/database/client.go` (Lines 266-317)

```go
// Complex multi-step update/insert/delete pattern
func (c *clientImpl) RefreshToolsForServer(serverName string, groupKind string, 
    tools ...*v1alpha2.MCPTool) error {
    
    // Step 1: Get existing tools
    existingTools, err := c.ListToolsForServer(serverName, groupKind)
    if err != nil {
        return err
    }
    
    // Step 2: Update or create new tools
    for _, tool := range tools {
        existingToolIndex := slices.IndexFunc(existingTools, func(t Tool) bool {
            return t.ID == tool.Name
        })
        if existingToolIndex != -1 {
            // Update existing tool
            existingTool := existingTools[existingToolIndex]
            existingTool.ServerName = serverName
            existingTool.GroupKind = groupKind
            existingTool.Description = tool.Description
            err = save(c.db, &existingTool)
            if err != nil {
                return err
            }
        } else {
            // Create new tool
            err = save(c.db, &Tool{
                ID:          tool.Name,
                ServerName:  serverName,
                GroupKind:   groupKind,
                Description: tool.Description,
            })
            if err != nil {
                return fmt.Errorf("failed to create tool %s: %v", tool.Name, err)
            }
        }
    }
    
    // Step 3: Delete tools no longer present
    for _, existingTool := range existingTools {
        if !slices.ContainsFunc(tools, func(t *v1alpha2.MCPTool) bool {
            return t.Name == existingTool.ID
        }) {
            err = delete[Tool](c.db,
                Clause{Key: "id", Value: existingTool.ID},
                Clause{Key: "server_name", Value: serverName},
                Clause{Key: "group_kind", Value: groupKind})
            if err != nil {
                return fmt.Errorf("failed to delete tool %s: %v", existingTool.ID, err)
            }
        }
    }
    return nil
}
```

**Pattern**: Three-phase operation (read, insert/update, delete) with error handling at each step.

---

## 4. ERROR HANDLING PATTERNS

### 4.1 Custom APIError Type

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/errors/errors.go`

```go
// Lines 9-26: APIError with error wrapping
type APIError struct {
    Code    int
    Message string
    Err     error
}

func (e *APIError) Error() string {
    if e.Err != nil {
        return fmt.Sprintf("%s: %v", e.Message, e.Err)
    }
    return e.Message
}

func (e *APIError) Unwrap() error {
    return e.Err
}

func (e *APIError) StatusCode() int {
    return e.Code
}

// Error factory functions - Lines 34-91
func NewBadRequestError(message string, err error) *APIError {
    return &APIError{
        Code:    http.StatusBadRequest,
        Message: message,
        Err:     err,
    }
}

func NewNotFoundError(message string, err error) *APIError {
    return &APIError{
        Code:    http.StatusNotFound,
        Message: message,
        Err:     err,
    }
}

func NewInternalServerError(message string, err error) *APIError {
    return &APIError{
        Code:    http.StatusInternalServerError,
        Message: message,
        Err:     err,
    }
}

func NewValidationError(message string, err error) *APIError {
    return &APIError{
        Code:    http.StatusUnprocessableEntity,
        Message: message,
        Err:     err,
    }
}

// Additional error types
func NewConflictError(message string, err error) *APIError { /* ... */ }
func NewNotImplementedError(message string, err error) *APIError { /* ... */ }
func NewForbiddenError(message string, err error) *APIError { /* ... */ }
```

**Key Features**:
- Implements Go error interface
- Implements Unwrap() for error wrapping
- Provides HTTP status code
- Maintains semantic meaning (message + HTTP status)
- Factory functions for common error types

### 4.2 Error Handler Middleware

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/middleware_error.go`

```go
// Lines 12-21: Error handling middleware
func errorHandlerMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ew := &errorResponseWriter{
            ResponseWriter: w,
            request:        r,
        }
        next.ServeHTTP(ew, r)
    })
}

// Lines 23-36: errorResponseWriter - wraps http.ResponseWriter
type errorResponseWriter struct {
    http.ResponseWriter
    request *http.Request
}

var _ handlers.ErrorResponseWriter = &errorResponseWriter{}
var _ http.Flusher = &errorResponseWriter{}

// Lines 38-67: RespondWithError - centralized error response
func (w *errorResponseWriter) RespondWithError(err error) {
    log := ctrllog.FromContext(w.request.Context())
    
    statusCode := http.StatusInternalServerError
    message := "Internal server error"
    detail := ""
    
    if apiErr, ok := err.(*errors.APIError); ok {
        statusCode = apiErr.Code
        message = apiErr.Message
        if apiErr.Err != nil {
            detail = apiErr.Err.Error()
            log.Error(apiErr.Err, message)
        } else {
            log.Info(message)
        }
    } else {
        detail = err.Error()
        log.Error(err, "Unhandled error")
    }
    
    responseMessage := message
    if detail != "" {
        responseMessage = message + ": " + detail
    }
    
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    json.NewEncoder(w).Encode(map[string]string{"error": responseMessage})
}
```

**Pattern**:
- Middleware wraps response writer
- Centralized error formatting
- Automatic HTTP status code selection
- Structured logging of errors
- JSON error responses

### 4.3 Handler Error Handling Flow

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/handlers/sessions.go` (Lines 75-95)

```go
// Standard error handling pattern in handlers
func (h *SessionsHandler) HandleListSessions(w ErrorResponseWriter, r *http.Request) {
    log := ctrllog.FromContext(r.Context()).WithName("sessions-handler").WithValues("operation", "list-db")
    
    // Error 1: Request validation
    userID, err := GetUserID(r)
    if err != nil {
        w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
        return
    }
    log = log.WithValues("userID", userID)
    
    // Error 2: Business logic
    log.V(1).Info("Listing sessions from database")
    sessions, err := h.DatabaseService.ListSessions(userID)
    if err != nil {
        w.RespondWithError(errors.NewInternalServerError("Failed to list sessions", err))
        return
    }
    
    // Success case
    log.Info("Successfully listed sessions", "count", len(sessions))
    data := api.NewResponse(sessions, "Successfully listed sessions", false)
    RespondWithJSON(w, http.StatusOK, data)
}
```

**Error Handling Hierarchy**:
1. Validate request parameters → BadRequestError
2. Check business logic constraints → ValidationError/NotFoundError
3. Handle database/system errors → InternalServerError
4. Permission checks → ForbiddenError

---

## 5. CODE ORGANIZATION AND FILE STRUCTURE

### 5.1 Directory Structure

```
go/
├── api/                          # Kubernetes CRD types
│   ├── v1alpha1/                # v1alpha1 API version
│   └── v1alpha2/                # Current API version (agents, model configs, MCP servers)
│
├── cmd/
│   └── controller/
│       └── main.go              # Entry point
│
├── internal/                    # Private packages
│   ├── a2a/                     # A2A protocol implementation
│   ├── adk/                     # Agent Development Kit types
│   ├── controller/              # Kubernetes controllers
│   │   ├── agent_controller.go
│   │   ├── modelconfig_controller.go
│   │   ├── remote_mcp_server_controller.go
│   │   ├── reconciler/          # Reconciliation logic
│   │   ├── translator/          # CRD to K8s resource translation
│   │   └── predicates/          # Watch predicates
│   ├── database/                # Data persistence
│   │   ├── models.go            # GORM models
│   │   ├── client.go            # Database operations
│   │   ├── manager.go           # Database lifecycle
│   │   ├── service.go           # Query helpers
│   │   └── fake/                # Mock client for testing
│   ├── httpserver/              # HTTP API
│   │   ├── server.go            # HTTP server setup
│   │   ├── middleware.go        # Request/response middleware
│   │   ├── middleware_error.go  # Error handling middleware
│   │   ├── errors/              # Error types
│   │   └── handlers/            # HTTP handlers by resource
│   │       ├── handlers.go      # Handler registry
│   │       ├── agents.go
│   │       ├── sessions.go
│   │       ├── tasks.go
│   │       ├── tools.go
│   │       ├── checkpoints.go
│   │       ├── memory.go
│   │       ├── helpers.go       # Shared handler utilities
│   │       └── *_test.go        # Handler tests
│   ├── utils/                   # Common utilities
│   ├── version/                 # Version info
│   └── goruntime/               # Runtime utilities
│
├── pkg/                         # Public packages
│   ├── auth/                    # Authentication/authorization
│   └── client/
│       └── api/                 # API client types
│
└── test/
    └── e2e/                     # End-to-end tests
```

### 5.2 Package Naming Conventions

**Private Packages** (`internal/`):
- Controller components: `controller`, `reconciler`, `translator`
- Database layer: `database`
- HTTP layer: `httpserver`
- Utilities: `utils`, `adk`, `a2a`

**Public Packages** (`pkg/`):
- Authentication: `auth`
- Client types: `client/api`

**File Naming**:
- Controller files: `{resource}_controller.go` (e.g., `agent_controller.go`)
- Handler files: `{resource}.go` (e.g., `sessions.go`, `agents.go`)
- Test files: `{filename}_test.go`
- Mock files: `mock_*.go` or `fake/*.go`

### 5.3 Import Organization

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/handlers/agents.go` (Lines 1-18)

```go
package handlers

import (
    "context"
    "net/http"
    
    // Kubernetes client libraries
    "github.com/go-logr/logr"
    "github.com/kagent-dev/kagent/go/api/v1alpha2"
    agent_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
    "github.com/kagent-dev/kagent/go/internal/httpserver/errors"
    "github.com/kagent-dev/kagent/go/internal/utils"
    "github.com/kagent-dev/kagent/go/pkg/auth"
    "github.com/kagent-dev/kagent/go/pkg/client/api"
    k8serrors "k8s.io/apimachinery/pkg/api/errors"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client"
    ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)
```

**Import Grouping Pattern**:
1. Standard library imports
2. External dependencies (Kubernetes, control-runtime)
3. Local package imports (internal, pkg)
4. Grouped by common functionality

---

## 6. NAMING CONVENTIONS

### 6.1 Constants

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/server.go` (Lines 21-41)

```go
const (
    APIPathHealth          = "/health"
    APIPathVersion         = "/version"
    APIPathModelConfig     = "/api/modelconfigs"
    APIPathSessions        = "/api/sessions"
    APIPathAgents          = "/api/agents"
    APIPathToolServers     = "/api/toolservers"
    APIPathToolServerTypes = "/api/toolservertypes"
)
```

**Pattern**: `APIPath{ResourceName}` - clear, descriptive constant names.

### 6.2 Interfaces

**File**: `/Users/cwalker/repos/kagent/go/internal/database/client.go` (Lines 15-68)

```go
type Client interface {
    // Store methods
    StoreFeedback(feedback *Feedback) error
    StoreSession(session *Session) error
    StoreAgent(agent *Agent) error
    StoreTask(task *protocol.Task) error
    
    // Delete methods
    DeleteSession(sessionName string, userID string) error
    DeleteAgent(agentID string) error
    
    // Get methods
    GetSession(name string, userID string) (*Session, error)
    GetAgent(name string) (*Agent, error)
    
    // List methods
    ListTools() ([]Tool, error)
    ListFeedback(userID string) ([]Feedback, error)
}

type clientImpl struct {
    db *gorm.DB
}

func NewClient(dbManager *Manager) Client {
    return &clientImpl{db: dbManager.db}
}
```

**Pattern**:
- `Client` - public interface
- `clientImpl` - private implementation
- Method naming: `{Verb}{Noun}` (Store, Delete, Get, List, etc.)
- Factory function: `NewClient` returns interface type

### 6.3 Functions and Methods

**Naming Patterns**:

```go
// Handlers
func NewSessionsHandler(base *Base) *SessionsHandler
func (h *SessionsHandler) HandleListSessions(w ErrorResponseWriter, r *http.Request)
func (h *SessionsHandler) HandleCreateSession(w ErrorResponseWriter, r *http.Request)

// Database operations
func (c *clientImpl) StoreSession(session *Session) error
func (c *clientImpl) ListSessions(userID string) ([]Session, error)
func (c *clientImpl) GetSession(sessionName string, userID string) (*Session, error)
func (c *clientImpl) DeleteSession(sessionName string, userID string) error

// Generic helpers
func list[T Model](db *gorm.DB, clauses ...Clause) ([]T, error)
func get[T Model](db *gorm.DB, clauses ...Clause) (*T, error)
func save[T Model](db *gorm.DB, model *T) error
func delete[T Model](db *gorm.DB, clauses ...Clause) error
```

**Patterns**:
- Constructors: `New{Type}`
- HTTP handlers: `Handle{Action}{Resource}`
- Database operations: `{Action}{Resource}` (Store, List, Get, Delete)
- Generic functions: lowercase with type parameters

### 6.4 Variables and Receivers

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/middleware.go` (Lines 11-34)

```go
func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        log := ctrllog.Log.WithName("http").WithValues(
            "method", r.Method,
            "path", r.URL.Path,
            "remote_addr", r.RemoteAddr,
        )
        
        if userID := r.URL.Query().Get("user_id"); userID != "" {
            log = log.WithValues("user_id", userID)
        }
        
        ww := newStatusResponseWriter(w)
        ctx := ctrllog.IntoContext(r.Context(), log)
        log.V(1).Info("Request started")
        next.ServeHTTP(ww, r.WithContext(ctx))
        log.Info("Request completed",
            "status", ww.status,
            "duration", time.Since(start),
        )
    })
}
```

**Variable Naming**:
- Short receiver names: `w`, `r`, `h`, `c` (1-2 characters)
- Descriptive variable names in function bodies
- Logging context: `log` (from controller-runtime)
- Response writers: `ww` for wrapped writer

### 6.5 Types and Structs

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/handlers/checkpoints.go` (Lines 14-69)

```go
// Handler type
type CheckpointsHandler struct {
    *Base
}

// Request payload types (PascalCase with Payload suffix or Request suffix)
type KAgentCheckpointPayload struct {
    ThreadID           string  `json:"thread_id"`
    CheckpointNS       string  `json:"checkpoint_ns"`
    CheckpointID       string  `json:"checkpoint_id"`
    ParentCheckpointID *string `json:"parent_checkpoint_id"`
    Checkpoint         string  `json:"checkpoint"`
    Metadata           string  `json:"metadata"`
    Type               string  `json:"type_"`
    Version            int     `json:"version"`
}

type KagentCheckpointWrite struct {
    Idx     int    `json:"idx"`
    Channel string `json:"channel"`
    Type    string `json:"type_"`
    Value   string `json:"value"`
}

type KAgentCheckpointTuple struct {
    ThreadID           string                        `json:"thread_id"`
    CheckpointNS       string                        `json:"checkpoint_ns"`
    CheckpointID       string                        `json:"checkpoint_id"`
    ParentCheckpointID *string                       `json:"parent_checkpoint_id"`
    Checkpoint         string                        `json:"checkpoint"`
    Metadata           string                        `json:"metadata"`
    Type               string                        `json:"type_"`
    Writes             *KAgentCheckpointWritePayload `json:"writes"`
}
```

**Type Naming Patterns**:
- Handler: `{Resource}Handler`
- API request: `{ActionName}Request` or `{TypeName}Payload`
- API response: `{TypeName}Response` or `{TypeName}Tuple`
- Database model: `{EntityName}` (no suffix)

### 6.6 JSON Tag Conventions

**File**: `/Users/cwalker/repos/kagent/go/internal/database/models.go` (Lines 13-24)

```go
type Agent struct {
    ID        string         `gorm:"primaryKey" json:"id"`
    CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
    
    Type string `gorm:"not null" json:"type"`
    Config *adk.AgentConfig `gorm:"type:json" json:"config"`
}
```

**JSON Tag Pattern**:
- Snake_case for JSON field names: `created_at`, `deleted_at`
- Omit empty optional fields: `json:"config,omitempty"`
- Match database column names
- Database tag comes first, JSON tag second

---

## 7. LOGGING PATTERNS

### 7.1 Logger Initialization and Context

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/handlers/sessions.go` (Lines 75-80)

```go
func (h *SessionsHandler) HandleListSessions(w ErrorResponseWriter, r *http.Request) {
    // Logger from request context with handler-specific values
    log := ctrllog.FromContext(r.Context()).
        WithName("sessions-handler").
        WithValues("operation", "list-db")
    
    // Add values as context emerges
    userID, err := GetUserID(r)
    if err != nil {
        w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
        return
    }
    log = log.WithValues("userID", userID)
}
```

**Pattern**:
- Get logger from request context: `ctrllog.FromContext(r.Context())`
- Add handler name: `.WithName("handler-name")`
- Add operation: `.WithValues("operation", "operation-name")`
- Add contextual values progressively: `log.WithValues(...)`

### 7.2 Verbosity Levels

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/middleware.go` (Lines 27-32)

```go
log.V(1).Info("Request started")
next.ServeHTTP(ww, r.WithContext(ctx))
log.Info("Request completed",
    "status", ww.status,
    "duration", time.Since(start),
)
```

**Verbosity Convention**:
- `log.Info()` - Always logged (default level)
- `log.V(1).Info()` - Verbose detail level
- `log.Error()` - Error level for failures

### 7.3 Error Logging

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/middleware_error.go` (Lines 38-67)

```go
func (w *errorResponseWriter) RespondWithError(err error) {
    if apiErr, ok := err.(*errors.APIError); ok {
        if apiErr.Err != nil {
            detail = apiErr.Err.Error()
            log.Error(apiErr.Err, message)  // Log the wrapped error
        } else {
            log.Info(message)                // Log as info if no wrapped error
        }
    } else {
        detail = err.Error()
        log.Error(err, "Unhandled error")  // Log unexpected errors
    }
}
```

**Pattern**: `log.Error(err, "context message", "key", "value")`

---

## 8. TESTING PATTERNS

### 8.1 Mock Generation

**File**: `/Users/cwalker/repos/kagent/go/internal/httpserver/handlers/mock_client_test.go`

The codebase uses interface mocking for testing. Key files:
- `handlers_test.go` - Tests for handlers
- `sessions_test.go` - Tests for session handler
- `mock_client_test.go` - Mock implementations for testing

**Pattern**:
```go
// Interface-based design enables easy mocking
type Client interface {
    StoreSession(session *Session) error
    ListSessions(userID string) ([]Session, error)
}

// Mock implementation for tests
type MockClient struct {
    mock.Mock
}

func (m *MockClient) StoreSession(session *Session) error {
    args := m.Called(session)
    return args.Error(0)
}
```

---

## 9. KEY ARCHITECTURAL PATTERNS

### 9.1 Dependency Injection

**Pattern**: Constructor-based DI with interfaces

```go
// ServerConfig holds all dependencies
type ServerConfig struct {
    Router            *mux.Router
    KubeClient        ctrl_client.Client
    DbClient          database.Client
    Authenticator     auth.AuthProvider
    Authorizer        auth.Authorizer
}

// Base struct embedded in all handlers
type Base struct {
    KubeClient         client.Client
    DatabaseService    database.Client
    Authorizer         auth.Authorizer
}

// Handler receives Base via embedding
type SessionsHandler struct {
    *Base
}
```

### 9.2 Factory Pattern

```go
// Factory functions for construction
func NewHTTPServer(config ServerConfig) (*HTTPServer, error)
func NewHandlers(kubeClient client.Client, ...) *Handlers
func NewSessionsHandler(base *Base) *SessionsHandler
func NewClient(dbManager *Manager) Client
```

### 9.3 Interface Segregation

```go
// Small, focused interfaces
type Client interface {
    StoreSession(session *Session) error
    ListSessions(userID string) ([]Session, error)
    // ... more methods
}

type ErrorResponseWriter interface {
    http.ResponseWriter
    RespondWithError(err error)
    Flush()
}

type Model interface {
    TableName() string
}
```

### 9.4 Middleware Pattern

```go
// Composable middleware stack
s.router.Use(auth.AuthnMiddleware(s.authenticator))
s.router.Use(contentTypeMiddleware)
s.router.Use(loggingMiddleware)
s.router.Use(errorHandlerMiddleware)
```

---

## 10. SUMMARY TABLE: NAMING CONVENTIONS

| Element | Pattern | Example | File Location |
|---------|---------|---------|-----------------|
| Constants | `APIPath{Resource}` | `APIPathSessions` | server.go:21-41 |
| Interfaces | PascalCase | `Client`, `ErrorResponseWriter` | client.go:15, middleware_error.go:21 |
| Implementations | `{InterfaceName}Impl` or lowercase | `clientImpl` | client.go:75 |
| Handlers | `{Resource}Handler` | `SessionsHandler` | sessions.go:18 |
| Handler Methods | `Handle{Action}{Resource}` | `HandleListSessions` | sessions.go:75 |
| Database Methods | `{Verb}{Noun}` | `StoreSession`, `ListSessions` | client.go:18-48 |
| Generic Functions | lowercase, type params | `list[T]`, `get[T]` | service.go:18-46 |
| Constructors | `New{Type}` | `NewSessionsHandler` | sessions.go:23 |
| Variables | Short names (1-2 char) | `w`, `r`, `h`, `c` | middleware.go:13 |
| JSON Fields | `snake_case` | `created_at`, `deleted_at` | models.go:15-16 |
| Type Names | `{EntityName}` | `Session`, `Agent` | models.go:58 |
| Payloads | `{Name}Payload` or `{Name}Request` | `KAgentCheckpointPayload` | checkpoints.go:26 |
| Responses | `{Name}Response` or `{Name}Tuple` | `AgentResponse` | api/types.go:85 |
| Loggers | `log` (from context) | `ctrllog.FromContext()` | sessions.go:76 |
| Receivers | Short names | `h *SessionsHandler`, `c *clientImpl` | sessions.go:32 |

---

## 11. RECOMMENDATIONS FOR CONSISTENCY

1. **Always use factory functions** (`New{Type}`) for construction
2. **Embed Base struct** in all HTTP handlers for shared dependencies
3. **Use interface types** in factory returns and function parameters
4. **Wrap errors with context** using `fmt.Errorf("context: %w", err)`
5. **Use snake_case** for JSON field names, PascalCase for Go identifiers
6. **Add logging context early** with handler name and operation
7. **Use generic query helpers** (list, get, save, delete) for type safety
8. **Implement error handlers** that return custom APIError types
9. **Use middleware** for cross-cutting concerns (auth, logging, error handling)
10. **Group imports** in standard library, external, and internal packages

---

## 12. FILE REFERENCE SUMMARY

| Area | Key Files |
|------|-----------|
| **API Design** | `server.go` (lines 21-232), `handlers.go` (lines 11-64) |
| **Database Models** | `models.go` (lines 1-217), `manager.go` (lines 14-161) |
| **Query Patterns** | `service.go` (lines 18-75), `client.go` (lines 336-624) |
| **Error Handling** | `errors/errors.go` (lines 9-92), `middleware_error.go` (lines 12-67) |
| **Handlers** | `handlers/handlers.go`, `handlers/sessions.go`, `handlers/agents.go` |
| **HTTP Middleware** | `middleware.go` (lines 11-76), `middleware_error.go` (lines 12-67) |
| **Main Entry** | `cmd/controller/main.go` (lines 29-40) |

