## API endpoint standards and conventions

### Router and Handler Structure

- **Router**: Use Gorilla Mux (`github.com/gorilla/mux`) for HTTP routing
- **Handler Organization**: Group related handlers in dedicated files (e.g., `agents.go`, `sessions.go`, `modelconfigs.go`)
- **Dependency Injection**: Pass dependencies via `ServerConfig` struct embedded in handler structs
- **Handler Pattern**: Embed a `Base` struct containing shared dependencies (database, clients, etc.)

Example from `go/internal/httpserver/server.go` (lines 21-232):
```go
type ServerConfig struct {
    AgentClient a2a.Client
    Database    *database.Service
    // ... other dependencies
}

type AgentHandler struct {
    Base
}

func (h *AgentHandler) SetConfig(config ServerConfig) {
    h.Base.SetConfig(config)
}
```

### Endpoint Naming Conventions

- **Path Constants**: Define as `APIPath{Resource}` (e.g., `APIPathAgents`, `APIPathSessions`)
- **Handler Methods**: Name as `Handle{Action}{Resource}` (e.g., `HandleListAgents`, `HandleGetAgent`, `HandleCreateSession`)
- **URL Structure**: Use plural nouns for collections (`/api/agents`, `/api/sessions`)
- **Resource IDs**: Use path parameters for resource identifiers (e.g., `/api/agents/{namespace}/{name}`)

### HTTP Method Usage

- **GET**: List resources or retrieve single resource (e.g., `GET /api/agents`, `GET /api/agents/{namespace}/{name}`)
- **POST**: Create new resources (e.g., `POST /api/agents`, `POST /api/sessions`)
- **PUT**: Full resource updates (if needed)
- **PATCH**: Partial resource updates (e.g., `PATCH /api/sessions/{id}`)
- **DELETE**: Remove resources (e.g., `DELETE /api/agents/{namespace}/{name}`)

### Request/Response Patterns

- **JSON**: All requests and responses use JSON format
- **Standard Response**: Wrap responses in a standard format with `data`, `error`, and `message` fields
- **Error Handling**: Use custom `APIError` types with appropriate HTTP status codes
- **Status Codes**:
  - 200 OK (successful GET, PATCH)
  - 201 Created (successful POST)
  - 204 No Content (successful DELETE)
  - 400 Bad Request (validation errors)
  - 404 Not Found (resource doesn't exist)
  - 500 Internal Server Error (server errors)

### Query Parameters

- **Filtering**: Support field-based filtering via query params (e.g., `?namespace=default`)
- **Pagination**: Use `limit` and `offset` or `page` and `page_size` parameters
- **User Context**: Use headers for user identification (`X-User-ID` header)

### Middleware

- **Error Middleware**: Centralized error handling that catches panics and formats errors consistently
- **CORS**: Configure CORS headers for cross-origin requests from UI
- **Logging**: Request/response logging for observability
- **Authentication**: Token validation via middleware (when enabled)

### Handler Implementation Pattern

```go
func (h *Handler) HandleAction(w http.ResponseWriter, r *http.Request) {
    // 1. Extract path/query parameters
    vars := mux.Vars(r)
    id := vars["id"]

    // 2. Parse request body (if applicable)
    var req RequestType
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        handleError(w, errors.NewBadRequest(err.Error()))
        return
    }

    // 3. Call business logic/database
    result, err := h.Database.GetSomething(r.Context(), id)
    if err != nil {
        handleError(w, err)
        return
    }

    // 4. Return JSON response
    respondJSON(w, http.StatusOK, result)
}
```

### Security

- **SQL Injection Prevention**: Use parameterized queries via GORM (never string concatenation)
- **Input Validation**: Validate all inputs before processing
- **User Isolation**: Filter data by user ID from authenticated context
- **Secret Management**: Never log or return sensitive data (API keys, tokens)
