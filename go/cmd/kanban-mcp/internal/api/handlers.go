package api

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/service"
)

// TasksHandler handles /api/tasks (GET list, POST create).
// Returns 501 Not Implemented; replaced in Step 8.
func TasksHandler(_ *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	}
}

// TaskHandler handles /api/tasks/{id} and /api/tasks/{id}/subtasks.
// Returns 404 for unknown task IDs; full implementation in Step 8.
func TaskHandler(_ *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}
}

// BoardHandler handles /api/board.
// Returns 501 Not Implemented; replaced in Step 8.
func BoardHandler(_ *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	}
}
