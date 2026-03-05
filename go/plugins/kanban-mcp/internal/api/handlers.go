package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"gorm.io/gorm"
)

// Board groups top-level tasks by status column.
type Board struct {
	Columns []Column `json:"columns"`
}

// Column holds tasks for a single status in the workflow.
type Column struct {
	Status string     `json:"status"`
	Tasks  []*db.Task `json:"tasks"`
}

// writeJSON encodes v as JSON with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeError sends a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// httpStatus maps service/DB errors to HTTP status codes.
func httpStatus(err error) int {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid status") ||
		strings.Contains(msg, "subtasks cannot have subtasks") ||
		strings.Contains(msg, "attachments can only be added to top-level tasks") ||
		strings.Contains(msg, "type must be") ||
		strings.Contains(msg, "filename and content required") ||
		strings.Contains(msg, "url required for link") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// parseID extracts the uint task ID and optional suffix from a path like
// /api/tasks/42 or /api/tasks/42/subtasks.
func parseID(path string) (uint, string, bool) {
	trimmed := strings.TrimPrefix(path, "/api/tasks/")
	parts := strings.SplitN(trimmed, "/", 2)
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	suffix := ""
	if len(parts) > 1 {
		suffix = "/" + parts[1]
	}
	return uint(id), suffix, true
}

// parseAttachmentID extracts the attachment ID from /api/attachments/42.
func parseAttachmentID(path string) (uint, bool) {
	trimmed := strings.TrimPrefix(path, "/api/attachments/")
	id, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}

// TasksHandler handles /api/tasks (GET list, POST create).
func TasksHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filter := service.TaskFilter{}
			if s := r.URL.Query().Get("status"); s != "" {
				ts := db.TaskStatus(s)
				filter.Status = &ts
			}
			if a := r.URL.Query().Get("assignee"); a != "" {
				filter.Assignee = &a
			}
			if l := r.URL.Query().Get("label"); l != "" {
				filter.Label = &l
			}
			tasks, err := svc.ListTasks(r.Context(), filter)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, tasks)

		case http.MethodPost:
			var body struct {
				Title       string   `json:"title"`
				Description string   `json:"description"`
				Status      string   `json:"status"`
				Labels      []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			req := service.CreateTaskRequest{
				Title:       body.Title,
				Description: body.Description,
				Status:      db.TaskStatus(body.Status),
				Labels:      body.Labels,
			}
			task, err := svc.CreateTask(r.Context(), req)
			if err != nil {
				writeError(w, httpStatus(err), err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, task)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// TaskHandler handles /api/tasks/{id} (GET, PUT, DELETE),
// /api/tasks/{id}/subtasks (GET, POST), and /api/tasks/{id}/attachments (POST).
func TaskHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, suffix, ok := parseID(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if suffix == "/subtasks" {
			handleSubtasks(w, r, svc, id)
			return
		}

		if suffix == "/attachments" {
			handleTaskAttachments(w, r, svc, id)
			return
		}

		if suffix != "" {
			http.NotFound(w, r)
			return
		}

		handleTask(w, r, svc, id)
	}
}

// AttachmentHandler handles /api/attachments/{id} (DELETE).
func AttachmentHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseAttachmentID(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := svc.DeleteAttachment(r.Context(), id); err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleTask dispatches methods for /api/tasks/{id}.
func handleTask(w http.ResponseWriter, r *http.Request, svc *service.TaskService, id uint) {
	switch r.Method {
	case http.MethodGet:
		task, err := svc.GetTask(r.Context(), id)
		if err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)

	case http.MethodPut:
		var body struct {
			Title           *string   `json:"title"`
			Description     *string   `json:"description"`
			Status          *string   `json:"status"`
			Assignee        *string   `json:"assignee"`
			Labels          *[]string `json:"labels"`
			UserInputNeeded *bool     `json:"user_input_needed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		req := service.UpdateTaskRequest{
			Title:           body.Title,
			Description:     body.Description,
			Assignee:        body.Assignee,
			Labels:          body.Labels,
			UserInputNeeded: body.UserInputNeeded,
		}
		if body.Status != nil {
			s := db.TaskStatus(*body.Status)
			req.Status = &s
		}
		task, err := svc.UpdateTask(r.Context(), id, req)
		if err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)

	case http.MethodDelete:
		if err := svc.DeleteTask(r.Context(), id); err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSubtasks dispatches methods for /api/tasks/{id}/subtasks.
func handleSubtasks(w http.ResponseWriter, r *http.Request, svc *service.TaskService, parentID uint) {
	switch r.Method {
	case http.MethodGet:
		pid := parentID
		tasks, err := svc.ListTasks(r.Context(), service.TaskFilter{ParentID: &pid})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tasks)

	case http.MethodPost:
		var body struct {
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Status      string   `json:"status"`
			Labels      []string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		req := service.CreateTaskRequest{
			Title:       body.Title,
			Description: body.Description,
			Status:      db.TaskStatus(body.Status),
			Labels:      body.Labels,
		}
		task, err := svc.CreateSubtask(r.Context(), parentID, req)
		if err != nil {
			writeError(w, httpStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, task)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTaskAttachments dispatches methods for /api/tasks/{id}/attachments.
func handleTaskAttachments(w http.ResponseWriter, r *http.Request, svc *service.TaskService, taskID uint) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Type     string `json:"type"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
		URL      string `json:"url"`
		Title    string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	req := service.CreateAttachmentRequest{
		Type:     db.AttachmentType(body.Type),
		Filename: body.Filename,
		Content:  body.Content,
		URL:      body.URL,
		Title:    body.Title,
	}
	attachment, err := svc.AddAttachment(r.Context(), taskID, req)
	if err != nil {
		writeError(w, httpStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

// BoardHandler handles GET /api/board.
func BoardHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tasks, err := svc.ListTasks(r.Context(), service.TaskFilter{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		byStatus := make(map[db.TaskStatus][]*db.Task)
		for _, t := range tasks {
			byStatus[t.Status] = append(byStatus[t.Status], t)
		}

		columns := make([]Column, 0, len(db.StatusWorkflow))
		for _, status := range db.StatusWorkflow {
			col := Column{
				Status: string(status),
				Tasks:  byStatus[status],
			}
			if col.Tasks == nil {
				col.Tasks = []*db.Task{}
			}
			columns = append(columns, col)
		}

		writeJSON(w, http.StatusOK, Board{Columns: columns})
	}
}
