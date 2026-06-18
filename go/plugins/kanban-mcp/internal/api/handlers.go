// Package api implements the kanban REST surface. Handlers are plain
// net/http.HandlerFunc factories — no external router — that dispatch on method
// and on simple URL-path inspection. Every handler delegates to the
// service.TaskService (which auto-broadcasts mutations to SSE clients) and maps
// service errors onto HTTP status codes:
//
//   - pgx.ErrNoRows (via service.IsNotFound) -> 404 Not Found
//   - validation errors (invalid status, nesting, attachment type) -> 400 Bad Request
//   - everything else -> 500 Internal Server Error
//
// The route shapes are:
//
//	GET,POST          /api/tasks
//	GET,PUT,DELETE    /api/tasks/{id}
//	GET,POST          /api/tasks/{id}/subtasks
//	PUT,DELETE        /api/subtasks/{id}
//	POST              /api/tasks/{id}/attachments
//	DELETE            /api/attachments/{id}
//	GET               /api/attachments/{id}/download
//	POST,DELETE       /api/tasks/{id}/attributes   (DELETE ?key={key})
//	GET               /api/board            (?board={key})
//	GET,POST          /api/boards
package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
)

// errorResponse is the JSON body returned by writeError.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON encodes v as JSON with the given status code. A non-nil v is
// required for any 2xx body; pass nil for empty bodies (e.g. 204).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	// Encoding errors at this point cannot be reported to the client (headers
	// are already flushed); they are intentionally ignored.
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON {"error": msg} body with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// writeServiceError maps a service-layer error onto an HTTP status. Not-found
// errors become 404; the remaining errors are treated as validation (400) when
// they are not pgx.ErrNoRows. Callers that can distinguish validation from
// internal failures should use writeError directly; this helper is the default
// for read/get paths where the only expected failure is not-found.
func writeServiceError(w http.ResponseWriter, err error) {
	if service.IsNotFound(err) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// writeMutationError maps a mutation error. Not-found -> 404, otherwise the
// error is assumed to be a validation failure (invalid status, nesting,
// attachment type, missing fields) and mapped to 400. Genuine DB/internal
// failures from the service are wrapped pgx errors that are not ErrNoRows; to
// avoid surfacing those as 400 they are detected separately by the caller when
// needed. For this server's surface, mutation failures that are not not-found
// are overwhelmingly validation errors, so 400 is the correct default.
func writeMutationError(w http.ResponseWriter, err error) {
	if service.IsNotFound(err) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

// ---------------------------------------------------------------------------
// Request bodies
// ---------------------------------------------------------------------------

// createTaskBody is the JSON body for POST /api/tasks. Board selects the target
// board for top-level creation; empty means the default board. ParentID, when set,
// creates a child Task under that Feature (board inherited from the Feature). Kind
// selects "feature" (default) or "task" for a top-level card; it is ignored when
// ParentID is set (child cards are always Tasks).
type createTaskBody struct {
	Board       string   `json:"board,omitempty"`
	ParentID    *int64   `json:"parent_id,omitempty"`
	Kind        string   `json:"kind,omitempty"` // "feature" (default) | "task"
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// createSubtaskBody is the JSON body for POST /api/tasks/{id}/subtasks.
type createSubtaskBody struct {
	Title string `json:"title"`
}

// updateSubtaskBody is the JSON body for PUT /api/subtasks/{id}. Pointer fields
// are nil when omitted, so only the provided fields are changed.
type updateSubtaskBody struct {
	Title *string `json:"title,omitempty"`
	Done  *bool   `json:"done,omitempty"`
}

// createBoardBody is the JSON body for POST /api/boards.
type createBoardBody struct {
	Key         string   `json:"key"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Columns     []string `json:"columns"`
	Subtasks    []string `json:"subtasks,omitempty"`
}

// updateTaskBody is the JSON body for PUT /api/tasks/{id}. Pointer fields are
// nil when the caller omits them, so only the provided fields are changed.
type updateTaskBody struct {
	Title           *string   `json:"title,omitempty"`
	Description     *string   `json:"description,omitempty"`
	Status          *string   `json:"status,omitempty"`
	Assignee        *string   `json:"assignee,omitempty"`
	Labels          *[]string `json:"labels,omitempty"`
	UserInputNeeded *bool     `json:"user_input_needed,omitempty"`
}

// createAttachmentBody is the JSON body for POST /api/tasks/{id}/attachments.
// For type=file, Content is base64-encoded file bytes.
type createAttachmentBody struct {
	Type     string `json:"type"`
	Filename string `json:"filename,omitempty"`
	Content  string `json:"content,omitempty"`
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
}

// setAttributeBody is the JSON body for POST /api/tasks/{id}/attributes.
type setAttributeBody struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// TasksHandler handles /api/tasks: GET (list top-level tasks with optional
// ?status=, ?assignee=, ?label= filters) and POST (create a top-level task).
func TasksHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listTasks(w, r, svc)
		case http.MethodPost:
			createTask(w, r, svc)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func listTasks(w http.ResponseWriter, r *http.Request, svc *service.TaskService) {
	var filter service.TaskFilter
	q := r.URL.Query()
	if s := q.Get("status"); s != "" {
		st := db.TaskStatus(s)
		filter.Status = &st
	}
	if a := q.Get("assignee"); a != "" {
		filter.Assignee = &a
	}
	if l := q.Get("label"); l != "" {
		filter.Label = &l
	}

	tasks, err := svc.ListTasks(r.Context(), q.Get("board"), filter)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func createTask(w http.ResponseWriter, r *http.Request, svc *service.TaskService) {
	var body createTaskBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	task, err := svc.CreateTask(r.Context(), body.Board, service.CreateTaskRequest{
		Title:       body.Title,
		Description: body.Description,
		Status:      db.TaskStatus(body.Status),
		Labels:      body.Labels,
		ParentID:    body.ParentID,
		Kind:        body.Kind,
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

// TaskHandler handles the /api/tasks/{id} family:
//
//	GET,PUT,DELETE   /api/tasks/{id}
//	GET,POST         /api/tasks/{id}/subtasks   (checklist items)
//	POST             /api/tasks/{id}/attachments
//	POST,DELETE      /api/tasks/{id}/attributes (DELETE ?key={key})
func TaskHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, sub, ok := parseTaskPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		switch sub {
		case "":
			taskByID(w, r, svc, id)
		case "subtasks":
			subtasks(w, r, svc, id)
		case "attachments":
			taskAttachments(w, r, svc, id)
		case "attributes":
			taskAttributes(w, r, svc, id)
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
	}
}

func taskByID(w http.ResponseWriter, r *http.Request, svc *service.TaskService, id int64) {
	switch r.Method {
	case http.MethodGet:
		task, err := svc.GetTask(r.Context(), id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, task)
	case http.MethodPut:
		var body updateTaskBody
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
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
			st := db.TaskStatus(*body.Status)
			req.Status = &st
		}
		task, err := svc.UpdateTask(r.Context(), id, req)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, task)
	case http.MethodDelete:
		if err := svc.DeleteTask(r.Context(), id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusNoContent, nil)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func subtasks(w http.ResponseWriter, r *http.Request, svc *service.TaskService, taskID int64) {
	switch r.Method {
	case http.MethodGet:
		subs, err := svc.ListSubtasks(r.Context(), taskID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, subs)
	case http.MethodPost:
		var body createSubtaskBody
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sub, err := svc.CreateSubtask(r.Context(), taskID, body.Title)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, sub)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// SubtaskHandler handles PUT and DELETE /api/subtasks/{id} for checklist items.
// PUT applies the provided fields (title and/or done); DELETE removes the item.
func SubtaskHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseSubtaskID(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		switch r.Method {
		case http.MethodPut:
			var body updateSubtaskBody
			if err := decodeJSON(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if body.Title == nil && body.Done == nil {
				writeError(w, http.StatusBadRequest, "nothing to update: provide title and/or done")
				return
			}
			var sub *service.Subtask
			var err error
			if body.Done != nil {
				sub, err = svc.ToggleSubtask(r.Context(), id, *body.Done)
				if err != nil {
					writeMutationError(w, err)
					return
				}
			}
			if body.Title != nil {
				sub, err = svc.UpdateSubtask(r.Context(), id, *body.Title)
				if err != nil {
					writeMutationError(w, err)
					return
				}
			}
			writeJSON(w, http.StatusOK, sub)
		case http.MethodDelete:
			if err := svc.DeleteSubtask(r.Context(), id); err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusNoContent, nil)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func taskAttachments(w http.ResponseWriter, r *http.Request, svc *service.TaskService, taskID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body createAttachmentBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	att, err := svc.AddAttachment(r.Context(), taskID, service.CreateAttachmentRequest{
		Type:     db.AttachmentType(body.Type),
		Filename: body.Filename,
		Content:  body.Content,
		URL:      body.URL,
		Title:    body.Title,
	})
	if err != nil {
		writeMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, att)
}

// taskAttributes handles /api/tasks/{id}/attributes: POST upserts an attribute
// ({key, value}); DELETE removes the attribute named by the ?key= query param.
func taskAttributes(w http.ResponseWriter, r *http.Request, svc *service.TaskService, taskID int64) {
	switch r.Method {
	case http.MethodPost:
		var body setAttributeBody
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		attr, err := svc.SetAttribute(r.Context(), taskID, body.Key, body.Value)
		if err != nil {
			writeMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, attr)
	case http.MethodDelete:
		key := r.URL.Query().Get("key")
		if key == "" {
			writeError(w, http.StatusBadRequest, "missing required query parameter: key")
			return
		}
		if err := svc.DeleteAttribute(r.Context(), taskID, key); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusNoContent, nil)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// AttachmentHandler handles the /api/attachments/{id} family:
//
//	DELETE   /api/attachments/{id}
//	GET      /api/attachments/{id}/download   (streams the decoded file bytes)
func AttachmentHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id, ok := parseAttachmentDownloadID(r.URL.Path); ok {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			serveAttachmentDownload(w, r, svc, id)
			return
		}

		id, ok := parseAttachmentID(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := svc.DeleteAttachment(r.Context(), id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusNoContent, nil)
	}
}

// serveAttachmentDownload streams a file attachment's decoded bytes with a
// Content-Disposition header that prompts the browser to download it.
func serveAttachmentDownload(w http.ResponseWriter, r *http.Request, svc *service.TaskService, id int64) {
	att, err := svc.GetAttachment(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if att.Type != db.AttachmentTypeFile {
		writeError(w, http.StatusBadRequest, "attachment is not a file")
		return
	}
	data, err := base64.StdEncoding.DecodeString(att.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "decoding attachment content")
		return
	}

	filename := att.Filename
	if filename == "" {
		filename = "download"
	}
	ctype := mime.TypeByExtension(filepath.Ext(filename))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// BoardHandler handles GET /api/board?board={key}, returning the full state of a
// single board (default board when ?board= is omitted).
func BoardHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		board, err := svc.GetBoard(r.Context(), r.URL.Query().Get("board"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, board)
	}
}

// BoardsHandler handles /api/boards: GET (list all boards' metadata) and POST
// (create a new board).
func BoardsHandler(svc *service.TaskService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			boards, err := svc.ListBoards(r.Context())
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, boards)
		case http.MethodPost:
			var body createBoardBody
			if err := decodeJSON(r, &body); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			board, err := svc.CreateBoard(r.Context(), service.CreateBoardRequest{
				Key:         body.Key,
				Name:        body.Name,
				Description: body.Description,
				Scope:       body.Scope,
				Owner:       body.Owner,
				Columns:     body.Columns,
				Subtasks:    body.Subtasks,
			})
			if err != nil {
				writeMutationError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, board)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// decodeJSON decodes the request body into v, rejecting unknown fields and
// returning a descriptive error on malformed input.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// parseTaskPath splits /api/tasks/{id}[/{sub}] into its numeric id and an
// optional sub-route ("subtasks", "attachments", or "attributes"). It returns
// ok=false for a missing/invalid id or a path with more than one trailing segment.
func parseTaskPath(path string) (id int64, sub string, ok bool) {
	rest := strings.TrimPrefix(path, "/api/tasks/")
	if rest == "" || rest == path {
		return 0, "", false
	}
	rest = strings.TrimSuffix(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, "", false
	}
	parsed, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	if len(parts) == 2 {
		sub = parts[1]
	}
	return parsed, sub, true
}

// parseAttachmentID extracts the numeric id from /api/attachments/{id}.
func parseAttachmentID(path string) (int64, bool) {
	return parseSingleID(path, "/api/attachments/")
}

// parseAttachmentDownloadID extracts {id} from /api/attachments/{id}/download.
func parseAttachmentDownloadID(path string) (int64, bool) {
	rest := strings.TrimPrefix(path, "/api/attachments/")
	if rest == path {
		return 0, false
	}
	rest = strings.TrimSuffix(rest, "/")
	idStr, ok := strings.CutSuffix(rest, "/download")
	if !ok || strings.Contains(idStr, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// parseSubtaskID extracts the numeric id from /api/subtasks/{id}.
func parseSubtaskID(path string) (int64, bool) {
	return parseSingleID(path, "/api/subtasks/")
}

// parseSingleID extracts a single numeric id segment after the given prefix,
// rejecting empty values and any nested sub-path.
func parseSingleID(path, prefix string) (int64, bool) {
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" || rest == path {
		return 0, false
	}
	rest = strings.TrimSuffix(rest, "/")
	if strings.Contains(rest, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
