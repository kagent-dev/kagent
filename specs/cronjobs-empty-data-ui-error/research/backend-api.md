# Research: CronJob Backend/API

## Key Files

| File | Purpose |
|------|---------|
| `go/api/v1alpha2/agentcronjob_types.go` | CRD type definitions |
| `go/core/internal/httpserver/handlers/cronjobs.go` | HTTP handlers |
| `go/core/internal/httpserver/server.go` (L278-283) | Route registration |
| `go/core/internal/controller/agentcronjob_controller.go` | Reconciliation logic |

## API Endpoints

- `GET /api/cronjobs` - List all (returns `[]AgentCronJob`)
- `GET /api/cronjobs/{namespace}/{name}` - Get one
- `POST /api/cronjobs` - Create
- `PUT /api/cronjobs/{namespace}/{name}` - Update
- `DELETE /api/cronjobs/{namespace}/{name}` - Delete

## Response Format

All endpoints use `StandardResponse[T]`:
```go
type StandardResponse[T any] struct {
    Error   bool   `json:"error"`
    Data    T      `json:"data,omitempty"`
    Message string `json:"message,omitempty"`
}
```

## Empty List Response

When no CronJobs exist, `cronJobList.Items` is an empty slice `[]`:
```json
{"error": false, "data": [], "message": "Successfully listed AgentCronJobs"}
```

**Important**: `data` field has `omitempty` JSON tag. If the Go slice is nil (not empty), the `data` field could be omitted from JSON entirely, resulting in:
```json
{"error": false, "message": "Successfully listed AgentCronJobs"}
```

This would cause `response.data` to be `undefined` in the UI, triggering the `!response.data` check and throwing an error.

## Storage

CronJobs are Kubernetes-native CRDs only - NOT stored in the database.
