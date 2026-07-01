// Package service implements the kanban TaskService: the central domain logic
// through which all task and attachment mutations flow. Every mutation persists
// via the sqlc-generated dbgen queries and then broadcasts the full board state
// so connected SSE clients stay in sync.
package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
)

// Task is the JSON-facing domain type returned by MCP tools and the REST API. It
// is assembled from the sqlc row type (dbgen.Task) plus eager-loaded checklist
// Subtasks, child tasks (Children), and Attachments where applicable.
//
// A task is either a Feature (Kind == "feature", ParentID == nil) or a Task
// (Kind == "task", ParentID points at a Feature). Both are full kanban cards;
// only Tasks carry checklist Subtasks. Children is populated by GetTask for a
// Feature so callers can see the Feature's child Tasks.
type Task struct {
	ID              int64         `json:"id"`
	Title           string        `json:"title"`
	Description     string        `json:"description,omitempty"`
	Status          db.TaskStatus `json:"status"`
	Kind            string        `json:"kind"` // "feature" | "task"
	Assignee        string        `json:"assignee,omitempty"`
	Labels          []string      `json:"labels,omitempty"`
	UserInputNeeded bool          `json:"user_input_needed"`
	ParentID        *int64        `json:"parent_id,omitempty"`
	BoardID         int64         `json:"board_id"`
	Subtasks        []*Subtask    `json:"subtasks,omitempty"`    // checklist items (Tasks only)
	Children        []*Task       `json:"children,omitempty"`    // child Tasks (Features only)
	Attachments     []*Attachment `json:"attachments,omitempty"` // file + link rows
	Attributes      []*Attribute  `json:"attributes,omitempty"`  // key/value rows
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// Subtask is a lightweight checklist item attached to a Task. Unlike a Task it
// has no status, assignee, labels, attachments, or board position: only a title
// and a done flag.
type Subtask struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Attachment is the JSON-facing domain type for a task file or link attachment.
// For type=file, Content holds the base64-encoded file bytes.
type Attachment struct {
	ID        int64             `json:"id"`
	TaskID    int64             `json:"task_id"`
	Type      db.AttachmentType `json:"type"`
	Filename  string            `json:"filename,omitempty"`
	Content   string            `json:"content,omitempty"`
	URL       string            `json:"url,omitempty"`
	Title     string            `json:"title,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Attribute is a simple key/value pair attached to a card. It is persisted as a
// type="attribute" row in kanban.attachment (title=key, content=value), sharing
// the attachment table but exposed to callers as a clean key/value pair.
type Attribute struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BoardState is the full state of a single board: its metadata plus one column
// per board-defined column, each holding that column's top-level tasks. It is the
// payload broadcast to SSE clients (scoped to the board) after every mutation.
type BoardState struct {
	Board   *Board   `json:"board"`
	Columns []Column `json:"columns"`
}

// Column holds the top-level tasks for a single board column.
type Column struct {
	Status db.TaskStatus `json:"status"`
	Tasks  []*Task       `json:"tasks"`
}

// TaskFilter narrows ListTasks results. A nil field means "no constraint".
type TaskFilter struct {
	Status   *db.TaskStatus
	Assignee *string
	Label    *string
	ParentID *int64 // nil = top-level only (WHERE parent_id IS NULL)
}

// CreateTaskRequest is the input for CreateTask. Status defaults to the board's
// first column when empty. When ParentID is set the new task is created as a
// child Task of that Feature (the board is inherited from the Feature). For a
// top-level card (ParentID nil) Kind selects "feature" (default) or "task" (a
// standalone Task with no parent); it is ignored when ParentID is set.
type CreateTaskRequest struct {
	Title       string
	Description string
	Status      db.TaskStatus
	Labels      []string
	ParentID    *int64
	Kind        string // "feature" (default) | "task"; top-level only
}

// UpdateTaskRequest carries partial updates. A nil pointer field is left
// unchanged.
type UpdateTaskRequest struct {
	Title           *string
	Description     *string
	Status          *db.TaskStatus
	Assignee        *string
	Labels          *[]string
	UserInputNeeded *bool
}

// Broadcaster receives the state of a single board (identified by boardKey) after
// every mutation that affects it. The SSE Hub implements this; a no-op stub is
// used for transports without live clients (e.g. stdio).
type Broadcaster interface {
	Broadcast(boardKey string, event any)
}

// NopBroadcaster is a Broadcaster that discards events. It lets TaskService run
// before the SSE Hub is wired in (and in stdio mode, which has no SSE clients).
type NopBroadcaster struct{}

// Broadcast implements Broadcaster and does nothing.
func (NopBroadcaster) Broadcast(string, any) {}

// TaskService is the central domain service for tasks and attachments.
type TaskService struct {
	q           *dbgen.Queries
	pool        *pgxpool.Pool
	broadcaster Broadcaster
}

// NewTaskService constructs a TaskService. If b is nil, a NopBroadcaster is used.
func NewTaskService(q *dbgen.Queries, pool *pgxpool.Pool, b Broadcaster) *TaskService {
	if b == nil {
		b = NopBroadcaster{}
	}
	return &TaskService{q: q, pool: pool, broadcaster: b}
}

// ListTasks returns tasks matching the filter. With filter.ParentID == nil the
// query returns the named board's cards (Features and child Tasks alike). When
// filter.ParentID is set the result is that Feature's child Tasks (the board is
// implied by the parent, so boardKey is ignored).
func (s *TaskService) ListTasks(ctx context.Context, boardKey string, filter TaskFilter) ([]*Task, error) {
	if filter.ParentID != nil {
		rows, err := s.q.ListChildTasks(ctx, filter.ParentID)
		if err != nil {
			return nil, fmt.Errorf("listing child tasks for parent %d: %w", *filter.ParentID, err)
		}
		return mapTasks(rows), nil
	}

	board, err := s.resolveBoard(ctx, boardKey)
	if err != nil {
		return nil, err
	}

	params := dbgen.ListBoardTasksParams{BoardID: board.ID}
	if filter.Status != nil {
		st := string(*filter.Status)
		params.Status = &st
	}
	params.Assignee = filter.Assignee
	params.Label = filter.Label

	rows, err := s.q.ListBoardTasks(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("listing tasks for board %q: %w", board.Key, err)
	}
	return mapTasks(rows), nil
}

// GetTask returns a single task with its checklist subtasks and attachments
// eager-loaded via separate queries (sqlc has no ORM-style preload). For a
// Feature it also loads its child Tasks (Children). Not-found is reported as a
// wrapped pgx.ErrNoRows.
func (s *TaskService) GetTask(ctx context.Context, id int64) (*Task, error) {
	row, err := s.q.GetTask(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", id, err)
	}
	t := mapTask(row)

	subs, err := s.q.ListSubtasksByTask(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("listing subtasks for task %d: %w", id, err)
	}
	t.Subtasks = mapSubtasks(subs)

	// A Feature exposes its child Tasks so a detail view can list them.
	if t.Kind == db.KindFeature {
		children, err := s.q.ListChildTasks(ctx, &t.ID)
		if err != nil {
			return nil, fmt.Errorf("listing child tasks for feature %d: %w", id, err)
		}
		t.Children = mapTasks(children)
	}

	atts, err := s.q.ListAttachments(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("listing attachments for task %d: %w", id, err)
	}
	t.Attachments, t.Attributes = splitAttachmentRows(atts)

	return t, nil
}

// CreateTask inserts a new task. When req.ParentID is nil it creates a Feature
// (top-level task) on the named board (empty boardKey = the default board). When
// req.ParentID is set it creates a child Task under that Feature, inheriting the
// Feature's board; the parent must itself be a Feature (a Task cannot parent
// another Task). Status defaults to the board's first column when empty and must
// otherwise be one of the board's columns.
func (s *TaskService) CreateTask(ctx context.Context, boardKey string, req CreateTaskRequest) (*Task, error) {
	if req.ParentID != nil {
		return s.createChildTask(ctx, *req.ParentID, req)
	}

	board, err := s.resolveBoard(ctx, boardKey)
	if err != nil {
		return nil, err
	}

	status := string(req.Status)
	if status == "" {
		status = board.Columns[0]
	}
	if !db.ValidColumn(board.Columns, status) {
		return nil, columnError(board, status)
	}

	kind := req.Kind
	if kind == "" {
		kind = db.KindFeature
	}
	if kind != db.KindFeature && kind != db.KindTask {
		return nil, fmt.Errorf("invalid kind %q: must be %q or %q", kind, db.KindFeature, db.KindTask)
	}

	row, err := s.q.CreateTask(ctx, dbgen.CreateTaskParams{
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Labels:      normalizeLabels(req.Labels),
		BoardID:     board.ID,
		Kind:        kind,
	})
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}

	task := mapTask(row)
	// Only Tasks carry checklist subtasks, so the board's subtask template is
	// applied to standalone Tasks (Features are containers and have none).
	if kind == db.KindTask {
		subs, err := s.applySubtaskTemplate(ctx, row.ID, board.Subtasks)
		if err != nil {
			return nil, err
		}
		task.Subtasks = subs
	}

	s.broadcastBoardKey(ctx, board.Key)
	return task, nil
}

// createChildTask inserts a child Task under the Feature parentID. The parent is
// fetched first so a missing parent surfaces as a wrapped pgx.ErrNoRows, and a
// parent that is itself a child Task is rejected (one level of nesting only).
func (s *TaskService) createChildTask(ctx context.Context, parentID int64, req CreateTaskRequest) (*Task, error) {
	parent, err := s.q.GetTask(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("getting parent task %d: %w", parentID, err)
	}
	if mapTask(parent).Kind != db.KindFeature {
		return nil, fmt.Errorf("a task's parent must be a feature")
	}

	board, err := s.q.GetBoardByID(ctx, parent.BoardID)
	if err != nil {
		return nil, fmt.Errorf("getting board for task %d: %w", parentID, err)
	}

	status := string(req.Status)
	if status == "" {
		status = board.Columns[0]
	}
	if !db.ValidColumn(board.Columns, status) {
		return nil, columnError(board, status)
	}

	pid := parentID
	row, err := s.q.CreateChildTask(ctx, dbgen.CreateChildTaskParams{
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Labels:      normalizeLabels(req.Labels),
		ParentID:    &pid,
		BoardID:     parent.BoardID,
	})
	if err != nil {
		return nil, fmt.Errorf("creating child task of feature %d: %w", parentID, err)
	}

	// A child card is always a Task, so it inherits the board's subtask template.
	task := mapTask(row)
	subs, err := s.applySubtaskTemplate(ctx, row.ID, board.Subtasks)
	if err != nil {
		return nil, err
	}
	task.Subtasks = subs

	s.broadcastBoardID(ctx, parent.BoardID)
	return task, nil
}

// applySubtaskTemplate creates one checklist subtask per template title on the
// given task, in order, returning the created subtasks. An empty template is a
// no-op (returns nil), so boards without a template behave exactly as before.
func (s *TaskService) applySubtaskTemplate(ctx context.Context, taskID int64, titles []string) ([]*Subtask, error) {
	if len(titles) == 0 {
		return nil, nil
	}
	subs := make([]*Subtask, 0, len(titles))
	for _, title := range titles {
		row, err := s.q.CreateSubtask(ctx, dbgen.CreateSubtaskParams{TaskID: taskID, Title: title})
		if err != nil {
			return nil, fmt.Errorf("creating template subtask %q for task %d: %w", title, taskID, err)
		}
		subs = append(subs, mapSubtask(row))
	}
	return subs, nil
}

// UpdateTask applies the non-nil fields of req to the task, preserving the rest.
func (s *TaskService) UpdateTask(ctx context.Context, id int64, req UpdateTaskRequest) (*Task, error) {
	current, err := s.q.GetTask(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", id, err)
	}

	params := dbgen.UpdateTaskParams{
		ID:              current.ID,
		Title:           current.Title,
		Description:     current.Description,
		Status:          current.Status,
		Assignee:        current.Assignee,
		Labels:          current.Labels,
		UserInputNeeded: current.UserInputNeeded,
	}
	if req.Title != nil {
		params.Title = *req.Title
	}
	if req.Description != nil {
		params.Description = *req.Description
	}
	if req.Status != nil {
		board, err := s.q.GetBoardByID(ctx, current.BoardID)
		if err != nil {
			return nil, fmt.Errorf("getting board for task %d: %w", id, err)
		}
		if !db.ValidColumn(board.Columns, string(*req.Status)) {
			return nil, columnError(board, string(*req.Status))
		}
		params.Status = string(*req.Status)
	}
	if req.Assignee != nil {
		params.Assignee = *req.Assignee
	}
	if req.Labels != nil {
		params.Labels = normalizeLabels(*req.Labels)
	}
	if req.UserInputNeeded != nil {
		params.UserInputNeeded = *req.UserInputNeeded
	}

	row, err := s.q.UpdateTask(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("updating task %d: %w", id, err)
	}

	if err := s.syncCloseDate(ctx, id, current.Status, row.Status); err != nil {
		return nil, fmt.Errorf("syncing close_date for task %d: %w", id, err)
	}

	s.broadcastBoardID(ctx, row.BoardID)
	return mapTask(row), nil
}

// MoveTask changes a task's status after validating that the target column
// belongs to the task's own board (tasks can only move between their board's
// predefined columns).
func (s *TaskService) MoveTask(ctx context.Context, id int64, status db.TaskStatus) (*Task, error) {
	current, err := s.q.GetTask(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", id, err)
	}
	board, err := s.q.GetBoardByID(ctx, current.BoardID)
	if err != nil {
		return nil, fmt.Errorf("getting board for task %d: %w", id, err)
	}
	if !db.ValidColumn(board.Columns, string(status)) {
		return nil, columnError(board, string(status))
	}

	row, err := s.q.MoveTask(ctx, dbgen.MoveTaskParams{
		ID:     id,
		Status: string(status),
	})
	if err != nil {
		return nil, fmt.Errorf("moving task %d: %w", id, err)
	}

	if err := s.syncCloseDate(ctx, id, current.Status, row.Status); err != nil {
		return nil, fmt.Errorf("syncing close_date for task %d: %w", id, err)
	}

	s.broadcastBoardID(ctx, row.BoardID)
	return mapTask(row), nil
}

// AssignTask sets a task's assignee. An empty assignee is valid and clears the
// current assignment.
func (s *TaskService) AssignTask(ctx context.Context, id int64, assignee string) (*Task, error) {
	row, err := s.q.AssignTask(ctx, dbgen.AssignTaskParams{
		ID:       id,
		Assignee: assignee,
	})
	if err != nil {
		return nil, fmt.Errorf("assigning task %d: %w", id, err)
	}

	s.broadcastBoardID(ctx, row.BoardID)
	return mapTask(row), nil
}

// ListSubtasks returns the checklist subtasks of a task in insertion order. A
// missing task simply yields an empty list (the FK guarantees rows only exist
// for real tasks).
func (s *TaskService) ListSubtasks(ctx context.Context, taskID int64) ([]*Subtask, error) {
	rows, err := s.q.ListSubtasksByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing subtasks for task %d: %w", taskID, err)
	}
	return mapSubtasks(rows), nil
}

// CreateSubtask adds a checklist subtask (title + done flag) to a Task. The
// owning task must exist and must be a Task, not a Feature: checklist subtasks
// attach to Tasks only. A missing task surfaces as a wrapped pgx.ErrNoRows.
func (s *TaskService) CreateSubtask(ctx context.Context, taskID int64, title string) (*Subtask, error) {
	task, err := s.q.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", taskID, err)
	}
	if task.ParentID == nil {
		return nil, fmt.Errorf("subtasks can only be added to tasks, not features")
	}

	row, err := s.q.CreateSubtask(ctx, dbgen.CreateSubtaskParams{
		TaskID: taskID,
		Title:  title,
	})
	if err != nil {
		return nil, fmt.Errorf("creating subtask of task %d: %w", taskID, err)
	}

	s.broadcastBoardID(ctx, task.BoardID)
	return mapSubtask(row), nil
}

// ToggleSubtask sets or clears a checklist subtask's done flag.
func (s *TaskService) ToggleSubtask(ctx context.Context, id int64, done bool) (*Subtask, error) {
	current, err := s.q.GetSubtask(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting subtask %d: %w", id, err)
	}
	row, err := s.q.SetSubtaskDone(ctx, dbgen.SetSubtaskDoneParams{ID: id, Done: done})
	if err != nil {
		return nil, fmt.Errorf("updating subtask %d: %w", id, err)
	}
	s.broadcastSubtaskBoard(ctx, current.TaskID)
	return mapSubtask(row), nil
}

// UpdateSubtask renames a checklist subtask.
func (s *TaskService) UpdateSubtask(ctx context.Context, id int64, title string) (*Subtask, error) {
	current, err := s.q.GetSubtask(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting subtask %d: %w", id, err)
	}
	row, err := s.q.UpdateSubtaskTitle(ctx, dbgen.UpdateSubtaskTitleParams{ID: id, Title: title})
	if err != nil {
		return nil, fmt.Errorf("updating subtask %d: %w", id, err)
	}
	s.broadcastSubtaskBoard(ctx, current.TaskID)
	return mapSubtask(row), nil
}

// DeleteSubtask removes a checklist subtask by id. A missing subtask is reported
// as a wrapped pgx.ErrNoRows (existence is checked first because the underlying
// DELETE is a no-op, not an error, when no row matches).
func (s *TaskService) DeleteSubtask(ctx context.Context, id int64) error {
	current, err := s.q.GetSubtask(ctx, id)
	if err != nil {
		return fmt.Errorf("getting subtask %d: %w", id, err)
	}
	if err := s.q.DeleteSubtask(ctx, id); err != nil {
		return fmt.Errorf("deleting subtask %d: %w", id, err)
	}
	s.broadcastSubtaskBoard(ctx, current.TaskID)
	return nil
}

// DeleteTask removes a task. Postgres ON DELETE CASCADE removes its subtasks and
// attachments. The delete and the post-delete board read run in one transaction.
func (s *TaskService) DeleteTask(ctx context.Context, id int64) error {
	// Resolve the board before deleting so the post-delete broadcast targets the
	// right board's subscribers.
	task, err := s.q.GetTask(ctx, id)
	if err != nil {
		return fmt.Errorf("getting task %d: %w", id, err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning delete tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback is a no-op after commit

	qtx := s.q.WithTx(tx)
	if err := qtx.DeleteTask(ctx, id); err != nil {
		return fmt.Errorf("deleting task %d: %w", id, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing delete of task %d: %w", id, err)
	}

	s.broadcastBoardID(ctx, task.BoardID)
	return nil
}

// CreateAttachmentRequest is the input for AddAttachment. The required fields
// depend on Type: type=file needs Filename and Content; type=link needs URL
// (Title is optional).
type CreateAttachmentRequest struct {
	Type     db.AttachmentType
	Filename string
	Content  string
	URL      string
	Title    string
}

// AddAttachment attaches a file or link to a task card (a Feature or a Task).
// The task must exist. Checklist subtasks cannot carry attachments because they
// are not tasks. The request is validated according to its Type before any DB
// write.
func (s *TaskService) AddAttachment(ctx context.Context, taskID int64, req CreateAttachmentRequest) (*Attachment, error) {
	task, err := s.q.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", taskID, err)
	}

	if !db.ValidUserAttachmentType(req.Type) {
		return nil, fmt.Errorf("invalid attachment type %q, valid types are %q and %q",
			req.Type, db.AttachmentTypeFile, db.AttachmentTypeLink)
	}

	switch req.Type {
	case db.AttachmentTypeFile:
		if req.Filename == "" {
			return nil, fmt.Errorf("filename is required for file attachments")
		}
		if !db.ValidFileExtension(req.Filename) {
			return nil, fmt.Errorf("unsupported file type %q; allowed extensions: %s",
				req.Filename, db.AllowedFileExtensionList())
		}
		if req.Content == "" {
			return nil, fmt.Errorf("content is required for file attachments")
		}
		if _, err := base64.StdEncoding.DecodeString(req.Content); err != nil {
			return nil, fmt.Errorf("file content must be base64-encoded: %w", err)
		}
	case db.AttachmentTypeLink:
		if req.URL == "" {
			return nil, fmt.Errorf("url is required for link attachments")
		}
	}

	row, err := s.q.AddAttachment(ctx, dbgen.AddAttachmentParams{
		TaskID:   taskID,
		Type:     string(req.Type),
		Filename: req.Filename,
		Url:      req.URL,
		Title:    req.Title,
		Content:  req.Content,
	})
	if err != nil {
		return nil, fmt.Errorf("adding attachment to task %d: %w", taskID, err)
	}

	s.broadcastBoardID(ctx, task.BoardID)
	return mapAttachment(row), nil
}

// GetAttachment returns a single file or link attachment by id. A missing
// attachment is reported as a wrapped pgx.ErrNoRows.
func (s *TaskService) GetAttachment(ctx context.Context, id int64) (*Attachment, error) {
	row, err := s.q.GetAttachment(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting attachment %d: %w", id, err)
	}
	return mapAttachment(row), nil
}

// DeleteAttachment removes an attachment by id. A missing attachment is reported
// as a wrapped pgx.ErrNoRows. Existence is checked first because the underlying
// DELETE is a no-op (not an error) when no row matches.
func (s *TaskService) DeleteAttachment(ctx context.Context, id int64) error {
	att, err := s.q.GetAttachment(ctx, id)
	if err != nil {
		return fmt.Errorf("getting attachment %d: %w", id, err)
	}

	if err := s.q.DeleteAttachment(ctx, id); err != nil {
		return fmt.Errorf("deleting attachment %d: %w", id, err)
	}

	// Resolve the owning task's board so the broadcast targets the right board.
	if task, err := s.q.GetTask(ctx, att.TaskID); err == nil {
		s.broadcastBoardID(ctx, task.BoardID)
	}
	return nil
}

// SetAttribute upserts a key/value attribute on a card (Feature or Task). The
// attribute is stored as a type="attribute" row in kanban.attachment (title=key,
// content=value). Setting an existing key replaces its value. The task must
// exist and the key must be non-empty.
func (s *TaskService) SetAttribute(ctx context.Context, taskID int64, key, value string) (*Attribute, error) {
	task, err := s.q.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", taskID, err)
	}
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("attribute key is required")
	}

	var row dbgen.Attachment
	existing, err := s.q.GetTaskAttribute(ctx, dbgen.GetTaskAttributeParams{TaskID: taskID, Title: key})
	switch {
	case err == nil:
		// Upsert: replace the value of the existing key.
		row, err = s.q.SetAttachmentContent(ctx, dbgen.SetAttachmentContentParams{ID: existing.ID, Content: value})
		if err != nil {
			return nil, fmt.Errorf("updating attribute %q on task %d: %w", key, taskID, err)
		}
	case errors.Is(err, pgx.ErrNoRows):
		row, err = s.q.AddAttachment(ctx, dbgen.AddAttachmentParams{
			TaskID:  taskID,
			Type:    string(db.AttachmentTypeAttribute),
			Title:   key,
			Content: value,
		})
		if err != nil {
			return nil, fmt.Errorf("adding attribute %q to task %d: %w", key, taskID, err)
		}
	default:
		return nil, fmt.Errorf("looking up attribute %q on task %d: %w", key, taskID, err)
	}

	s.broadcastBoardID(ctx, task.BoardID)
	return mapAttribute(row), nil
}

// DeleteAttribute removes the attribute with the given key from a card. A missing
// key (or task) is reported as a wrapped pgx.ErrNoRows so callers map it to 404.
func (s *TaskService) DeleteAttribute(ctx context.Context, taskID int64, key string) error {
	row, err := s.q.GetTaskAttribute(ctx, dbgen.GetTaskAttributeParams{TaskID: taskID, Title: key})
	if err != nil {
		return fmt.Errorf("getting attribute %q on task %d: %w", key, taskID, err)
	}
	if err := s.q.DeleteAttachment(ctx, row.ID); err != nil {
		return fmt.Errorf("deleting attribute %q on task %d: %w", key, taskID, err)
	}
	if task, err := s.q.GetTask(ctx, taskID); err == nil {
		s.broadcastBoardID(ctx, task.BoardID)
	}
	return nil
}

// GetBoard returns the full state of the named board (empty boardKey = the
// default board): every card (Feature or Task) grouped into the board's columns,
// with checklist subtasks and attachments eager-loaded. It is the snapshot used
// by the SSE Hub on client connect and by the get_board MCP tool / REST endpoint.
func (s *TaskService) GetBoard(ctx context.Context, boardKey string) (*BoardState, error) {
	board, err := s.resolveBoard(ctx, boardKey)
	if err != nil {
		return nil, err
	}
	return s.buildBoardState(ctx, board)
}

// buildBoardState assembles the full state of one board: its metadata plus one
// column per board-defined column. Features and Tasks are flat cards grouped by
// status; each card has its checklist subtasks and attachments eager-loaded
// (Features have no checklist subtasks, so that load returns empty for them).
func (s *TaskService) buildBoardState(ctx context.Context, board dbgen.Board) (*BoardState, error) {
	rows, err := s.q.ListBoardTasks(ctx, dbgen.ListBoardTasksParams{BoardID: board.ID})
	if err != nil {
		return nil, fmt.Errorf("listing tasks for board %q: %w", board.Key, err)
	}

	byStatus := make(map[string][]*Task, len(board.Columns))
	for _, row := range rows {
		t := mapTask(row)

		subs, err := s.q.ListSubtasksByTask(ctx, t.ID)
		if err != nil {
			return nil, fmt.Errorf("listing subtasks for task %d: %w", t.ID, err)
		}
		t.Subtasks = mapSubtasks(subs)

		atts, err := s.q.ListAttachments(ctx, t.ID)
		if err != nil {
			return nil, fmt.Errorf("listing attachments for task %d: %w", t.ID, err)
		}
		t.Attachments, t.Attributes = splitAttachmentRows(atts)

		byStatus[string(t.Status)] = append(byStatus[string(t.Status)], t)
	}

	state := &BoardState{
		Board:   mapBoard(board),
		Columns: make([]Column, 0, len(board.Columns)),
	}
	for _, col := range board.Columns {
		state.Columns = append(state.Columns, Column{
			Status: db.TaskStatus(col),
			Tasks:  byStatus[col],
		})
	}
	return state, nil
}

// broadcastBoardKey builds the named board's state and broadcasts it to that
// board's subscribers. Broadcast failures are non-fatal for the mutation that
// triggered them: the write already succeeded.
func (s *TaskService) broadcastBoardKey(ctx context.Context, boardKey string) {
	board, err := s.q.GetBoardByKey(ctx, boardKey)
	if err != nil {
		return
	}
	s.broadcastBoardState(ctx, board)
}

// broadcastBoardID is broadcastBoardKey addressed by board id, used when a
// mutation only has the task's board_id at hand.
func (s *TaskService) broadcastBoardID(ctx context.Context, boardID int64) {
	board, err := s.q.GetBoardByID(ctx, boardID)
	if err != nil {
		return
	}
	s.broadcastBoardState(ctx, board)
}

// broadcastSubtaskBoard resolves the board owning the subtask's task and
// broadcasts that board's state, used after a checklist subtask mutation.
func (s *TaskService) broadcastSubtaskBoard(ctx context.Context, taskID int64) {
	task, err := s.q.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	s.broadcastBoardID(ctx, task.BoardID)
}

// broadcastBoardState builds and broadcasts a single board's state.
func (s *TaskService) broadcastBoardState(ctx context.Context, board dbgen.Board) {
	state, err := s.buildBoardState(ctx, board)
	if err != nil {
		// The mutation succeeded; a failed board read should not fail it. Send a
		// refresh signal carrying just the board metadata.
		s.broadcaster.Broadcast(board.Key, &BoardState{Board: mapBoard(board), Columns: []Column{}})
		return
	}
	s.broadcaster.Broadcast(board.Key, state)
}

// columnError builds a descriptive error for a status that is not one of a
// board's columns.
func columnError(board dbgen.Board, status string) error {
	return fmt.Errorf("invalid column %q for board %q; valid columns are %v", status, board.Key, board.Columns)
}

// IsNotFound reports whether err is (or wraps) the pgx no-rows sentinel.
func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// normalizeLabels returns a non-nil label slice suitable for the
// `labels TEXT[] NOT NULL` column: nil becomes an empty (not NULL) slice, and
// duplicates are removed case-insensitively while preserving first-seen order
// and original casing (per the label rules in design.md).
func normalizeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		key := strings.ToLower(l)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, l)
	}
	return out
}

// mapTask converts a dbgen.Task row into the API Task domain type. Kind is read
// from the persisted column so a top-level card may be either a Feature or a
// standalone Task; legacy rows with an empty kind fall back to deriving it from
// parent_id (child = Task, top-level = Feature).
func mapTask(row dbgen.Task) *Task {
	kind := row.Kind
	if kind == "" {
		kind = db.KindFeature
		if row.ParentID != nil {
			kind = db.KindTask
		}
	}
	return &Task{
		ID:              row.ID,
		Title:           row.Title,
		Description:     row.Description,
		Status:          db.TaskStatus(row.Status),
		Kind:            kind,
		Assignee:        row.Assignee,
		Labels:          row.Labels,
		UserInputNeeded: row.UserInputNeeded,
		ParentID:        row.ParentID,
		BoardID:         row.BoardID,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

// closeDateKey is the attribute key whose value the service auto-manages: it is
// set to the current date (YYYY-MM-DD) when a card enters the Done column and
// removed when it leaves Done. Other date attributes (e.g. due_date) are user-set.
const closeDateKey = "close_date"

// syncCloseDate keeps the auto-managed close_date attribute in step with a card's
// status: entering Done stamps today's date, leaving Done removes it. A status
// change that does not cross the Done boundary leaves any existing value intact.
func (s *TaskService) syncCloseDate(ctx context.Context, taskID int64, oldStatus, newStatus string) error {
	done := string(db.StatusDone)
	switch {
	case newStatus == done && oldStatus != done:
		return s.upsertAttributeRow(ctx, taskID, closeDateKey, time.Now().UTC().Format("2006-01-02"))
	case newStatus != done && oldStatus == done:
		return s.deleteAttributeRow(ctx, taskID, closeDateKey)
	default:
		return nil
	}
}

// upsertAttributeRow sets key=value on a card without broadcasting (the caller is
// expected to broadcast once its own mutation completes). It mirrors SetAttribute's
// persistence but omits the GetTask/validation/broadcast steps.
func (s *TaskService) upsertAttributeRow(ctx context.Context, taskID int64, key, value string) error {
	existing, err := s.q.GetTaskAttribute(ctx, dbgen.GetTaskAttributeParams{TaskID: taskID, Title: key})
	switch {
	case err == nil:
		_, err = s.q.SetAttachmentContent(ctx, dbgen.SetAttachmentContentParams{ID: existing.ID, Content: value})
		return err
	case errors.Is(err, pgx.ErrNoRows):
		_, err = s.q.AddAttachment(ctx, dbgen.AddAttachmentParams{
			TaskID:  taskID,
			Type:    string(db.AttachmentTypeAttribute),
			Title:   key,
			Content: value,
		})
		return err
	default:
		return err
	}
}

// deleteAttributeRow removes key from a card without broadcasting. A missing key
// is not an error.
func (s *TaskService) deleteAttributeRow(ctx context.Context, taskID int64, key string) error {
	existing, err := s.q.GetTaskAttribute(ctx, dbgen.GetTaskAttributeParams{TaskID: taskID, Title: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.q.DeleteAttachment(ctx, existing.ID)
}

// mapTasks maps a slice of dbgen rows to API Task domain types.
func mapTasks(rows []dbgen.Task) []*Task {
	out := make([]*Task, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapTask(row))
	}
	return out
}

// mapSubtask converts a dbgen.Subtask row into the API Subtask domain type.
func mapSubtask(row dbgen.Subtask) *Subtask {
	return &Subtask{
		ID:        row.ID,
		TaskID:    row.TaskID,
		Title:     row.Title,
		Done:      row.Done,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// mapSubtasks maps a slice of subtask rows.
func mapSubtasks(rows []dbgen.Subtask) []*Subtask {
	out := make([]*Subtask, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapSubtask(row))
	}
	return out
}

// mapAttachment converts a dbgen.Attachment row into the API Attachment type.
func mapAttachment(row dbgen.Attachment) *Attachment {
	return &Attachment{
		ID:        row.ID,
		TaskID:    row.TaskID,
		Type:      db.AttachmentType(row.Type),
		Filename:  row.Filename,
		Content:   row.Content,
		URL:       row.Url,
		Title:     row.Title,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// mapAttribute converts a type="attribute" attachment row into an Attribute,
// mapping title→Key and content→Value.
func mapAttribute(row dbgen.Attachment) *Attribute {
	return &Attribute{
		ID:        row.ID,
		TaskID:    row.TaskID,
		Key:       row.Title,
		Value:     row.Content,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// splitAttachmentRows partitions the rows of kanban.attachment for a task into
// file/link attachments and key/value attributes, mapping each to its domain
// type. Both slices are non-nil so JSON omitempty drops empty ones cleanly.
func splitAttachmentRows(rows []dbgen.Attachment) ([]*Attachment, []*Attribute) {
	atts := make([]*Attachment, 0, len(rows))
	attrs := make([]*Attribute, 0)
	for _, row := range rows {
		if db.AttachmentType(row.Type) == db.AttachmentTypeAttribute {
			attrs = append(attrs, mapAttribute(row))
			continue
		}
		atts = append(atts, mapAttachment(row))
	}
	return atts, attrs
}
