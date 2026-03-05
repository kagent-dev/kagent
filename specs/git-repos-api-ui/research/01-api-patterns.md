# HTTP API Patterns

## Server Structure
- **Router:** Gorilla `mux` (`go/internal/httpserver/server.go`)
- **Handlers:** All embed `*Base` with `KubeClient`, `DatabaseService`, `Authorizer`
- **Factory:** `NewHandlers(...)` in `go/internal/httpserver/handlers/handlers.go`

## Route Registration
```go
s.router.HandleFunc("/api/{resource}", handler).Methods(http.MethodGet)
s.router.HandleFunc("/api/{resource}/{namespace}/{name}", handler).Methods(http.MethodGet)
```

## Handler Pattern
```go
func (h *Handler) HandleList(w ErrorResponseWriter, r *http.Request) {
    // 1. Auth check
    if err := Check(h.Authorizer, r, auth.Resource{Type: "..."}); err != nil { ... }
    // 2. Parse params (GetPathParam, DecodeJSONBody)
    // 3. K8s or DB operation
    // 4. RespondWithJSON(w, http.StatusOK, api.NewResponse(data, msg, false))
}
```

## Two Data Sources
1. **K8s API** (Agents, Models, ModelConfigs) — `h.KubeClient.List/Get/Create/Update/Delete`
2. **Database** (Sessions, Tasks, Tools) — `h.DatabaseService.List/Get/Store/Delete`

## Error Handling
- `errors.NewBadRequestError(msg, err)` → 400
- `errors.NewNotFoundError(msg, err)` → 404
- `errors.NewInternalServerError(msg, err)` → 500
- `w.RespondWithError(err)` writes JSON error response

## Middleware Stack
1. AuthnMiddleware → contentTypeMiddleware → loggingMiddleware → errorHandlerMiddleware

## Key Files
| File | Purpose |
|------|---------|
| `go/internal/httpserver/server.go` | Router, routes, middleware |
| `go/internal/httpserver/handlers/handlers.go` | Handler factory |
| `go/internal/httpserver/handlers/helpers.go` | DecodeJSONBody, GetPathParam, RespondWithJSON |
| `go/internal/httpserver/errors/errors.go` | APIError types |
