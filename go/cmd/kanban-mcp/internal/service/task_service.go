package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/db"
	"gorm.io/gorm"
)

// TaskFilter defines filters for listing tasks.
type TaskFilter struct {
	Status   *db.TaskStatus
	Assignee *string
	Label    *string // nil = all labels; set to filter tasks containing this label
	ParentID *uint   // nil = top-level only (WHERE parent_id IS NULL)
}

// CreateTaskRequest holds the data for creating a new task.
type CreateTaskRequest struct {
	Title       string
	Description string
	Status      db.TaskStatus // defaults to StatusInbox if empty
	Labels      []string
}

// UpdateTaskRequest holds fields for updating an existing task.
type UpdateTaskRequest struct {
	Title           *string
	Description     *string
	Status          *db.TaskStatus
	Assignee        *string
	Labels          *[]string // nil = no change; non-nil replaces existing labels
	UserInputNeeded *bool
}

// CreateAttachmentRequest holds the data for adding an attachment to a task.
type CreateAttachmentRequest struct {
	Type     db.AttachmentType // "file" or "link"
	Filename string            // required for type=file
	Content  string            // required for type=file
	URL      string            // required for type=link
	Title    string            // optional for type=link
}

// Broadcaster is an interface for broadcasting board change events.
type Broadcaster interface {
	Broadcast(event interface{})
}

// TaskService provides CRUD operations for tasks.
type TaskService struct {
	db          *gorm.DB
	broadcaster Broadcaster
}

// NewTaskService creates a new TaskService.
func NewTaskService(db *gorm.DB, b Broadcaster) *TaskService {
	return &TaskService{db: db, broadcaster: b}
}

// ListTasks returns tasks matching the filter.
// When filter.ParentID is nil, only top-level tasks (parent_id IS NULL) are returned.
func (s *TaskService) ListTasks(ctx context.Context, filter TaskFilter) ([]*db.Task, error) {
	q := s.db.WithContext(ctx)

	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}
	if filter.Assignee != nil {
		q = q.Where("assignee = ?", *filter.Assignee)
	}
	if filter.ParentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *filter.ParentID)
	}

	var tasks []*db.Task
	if err := q.Preload("Subtasks").Preload("Attachments").Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	// Apply label filter in-memory (JSON column not easily filterable in SQL across SQLite/Postgres)
	if filter.Label != nil {
		label := strings.ToLower(*filter.Label)
		filtered := make([]*db.Task, 0)
		for _, t := range tasks {
			for _, l := range t.Labels {
				if strings.ToLower(l) == label {
					filtered = append(filtered, t)
					break
				}
			}
		}
		tasks = filtered
	}

	return tasks, nil
}

// GetTask returns a task by ID with its subtasks and attachments preloaded.
// Returns a wrapped gorm.ErrRecordNotFound if the task does not exist.
func (s *TaskService) GetTask(ctx context.Context, id uint) (*db.Task, error) {
	var task db.Task
	if err := s.db.WithContext(ctx).Preload("Subtasks").Preload("Attachments").First(&task, id).Error; err != nil {
		return nil, fmt.Errorf("task %d not found: %w", id, err)
	}
	return &task, nil
}

// CreateTask creates a new task. Status defaults to StatusInbox if empty.
func (s *TaskService) CreateTask(ctx context.Context, req CreateTaskRequest) (*db.Task, error) {
	status := req.Status
	if status == "" {
		status = db.StatusInbox
	}

	task := &db.Task{
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Labels:      deduplicateLabels(req.Labels),
	}

	if err := s.db.WithContext(ctx).Create(task).Error; err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	s.broadcaster.Broadcast(task)
	return task, nil
}

// UpdateTask updates an existing task's fields.
func (s *TaskService) UpdateTask(ctx context.Context, id uint, req UpdateTaskRequest) (*db.Task, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Title != nil {
		task.Title = *req.Title
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Status != nil {
		if !db.ValidStatus(*req.Status) {
			return nil, fmt.Errorf("invalid status %q: valid statuses are %v", *req.Status, db.StatusWorkflow)
		}
		task.Status = *req.Status
	}
	if req.Assignee != nil {
		task.Assignee = *req.Assignee
	}
	if req.Labels != nil {
		task.Labels = deduplicateLabels(*req.Labels)
	}
	if req.UserInputNeeded != nil {
		task.UserInputNeeded = *req.UserInputNeeded
	}

	if err := s.db.WithContext(ctx).Save(task).Error; err != nil {
		return nil, fmt.Errorf("failed to update task %d: %w", id, err)
	}

	s.broadcaster.Broadcast(task)
	return task, nil
}

// MoveTask changes a task's status. Returns error for invalid status without writing to DB.
func (s *TaskService) MoveTask(ctx context.Context, id uint, status db.TaskStatus) (*db.Task, error) {
	if !db.ValidStatus(status) {
		return nil, fmt.Errorf("invalid status %q: valid statuses are %v", status, db.StatusWorkflow)
	}

	task, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}

	task.Status = status
	if err := s.db.WithContext(ctx).Save(task).Error; err != nil {
		return nil, fmt.Errorf("failed to move task %d: %w", id, err)
	}

	s.broadcaster.Broadcast(task)
	return task, nil
}

// AssignTask sets the assignee for a task. An empty string clears the assignment.
func (s *TaskService) AssignTask(ctx context.Context, id uint, assignee string) (*db.Task, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}

	task.Assignee = assignee
	if err := s.db.WithContext(ctx).Save(task).Error; err != nil {
		return nil, fmt.Errorf("failed to assign task %d: %w", id, err)
	}

	s.broadcaster.Broadcast(task)
	return task, nil
}

// CreateSubtask creates a new subtask under parentID.
// Returns an error if the parent does not exist or is itself a subtask (one-level nesting only).
func (s *TaskService) CreateSubtask(ctx context.Context, parentID uint, req CreateTaskRequest) (*db.Task, error) {
	parent, err := s.GetTask(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("parent task %d not found: %w", parentID, err)
	}
	if parent.ParentID != nil {
		return nil, fmt.Errorf("subtasks cannot have subtasks")
	}

	status := req.Status
	if status == "" {
		status = db.StatusInbox
	}

	task := &db.Task{
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Labels:      deduplicateLabels(req.Labels),
		ParentID:    &parentID,
	}

	if err := s.db.WithContext(ctx).Create(task).Error; err != nil {
		return nil, fmt.Errorf("failed to create subtask: %w", err)
	}

	s.broadcaster.Broadcast(task)
	return task, nil
}

// DeleteTask deletes a task and all its subtasks and attachments.
func (s *TaskService) DeleteTask(ctx context.Context, id uint) error {
	if _, err := s.GetTask(ctx, id); err != nil {
		return err
	}

	// Delete attachments on the task
	if err := s.db.WithContext(ctx).Where("task_id = ?", id).Delete(&db.Attachment{}).Error; err != nil {
		return fmt.Errorf("failed to delete attachments of task %d: %w", id, err)
	}

	// Delete attachments on subtasks
	var subtaskIDs []uint
	s.db.WithContext(ctx).Model(&db.Task{}).Where("parent_id = ?", id).Pluck("id", &subtaskIDs)
	if len(subtaskIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("task_id IN ?", subtaskIDs).Delete(&db.Attachment{}).Error; err != nil {
			return fmt.Errorf("failed to delete subtask attachments of task %d: %w", id, err)
		}
	}

	// Delete subtasks
	if err := s.db.WithContext(ctx).Where("parent_id = ?", id).Delete(&db.Task{}).Error; err != nil {
		return fmt.Errorf("failed to delete subtasks of task %d: %w", id, err)
	}

	// Delete the task itself
	if err := s.db.WithContext(ctx).Delete(&db.Task{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete task %d: %w", id, err)
	}

	s.broadcaster.Broadcast(nil)
	return nil
}

// AddAttachment adds an attachment to a top-level task.
// Returns an error if the task is a subtask or if validation fails.
func (s *TaskService) AddAttachment(ctx context.Context, taskID uint, req CreateAttachmentRequest) (*db.Attachment, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.ParentID != nil {
		return nil, fmt.Errorf("attachments can only be added to top-level tasks")
	}

	switch req.Type {
	case db.AttachmentTypeFile:
		if req.Filename == "" || req.Content == "" {
			return nil, fmt.Errorf("filename and content required for file attachments")
		}
	case db.AttachmentTypeLink:
		if req.URL == "" {
			return nil, fmt.Errorf("url required for link attachments")
		}
	default:
		return nil, fmt.Errorf("type must be 'file' or 'link'")
	}

	attachment := &db.Attachment{
		TaskID:   taskID,
		Type:     req.Type,
		Filename: req.Filename,
		Content:  req.Content,
		URL:      req.URL,
		Title:    req.Title,
	}

	if err := s.db.WithContext(ctx).Create(attachment).Error; err != nil {
		return nil, fmt.Errorf("failed to create attachment: %w", err)
	}

	s.broadcaster.Broadcast(attachment)
	return attachment, nil
}

// DeleteAttachment deletes an attachment by ID.
func (s *TaskService) DeleteAttachment(ctx context.Context, id uint) error {
	var attachment db.Attachment
	if err := s.db.WithContext(ctx).First(&attachment, id).Error; err != nil {
		return fmt.Errorf("attachment %d not found: %w", id, err)
	}

	if err := s.db.WithContext(ctx).Delete(&attachment).Error; err != nil {
		return fmt.Errorf("failed to delete attachment %d: %w", id, err)
	}

	s.broadcaster.Broadcast(nil)
	return nil
}

// deduplicateLabels removes duplicate labels while preserving order.
func deduplicateLabels(labels []string) db.StringSlice {
	if labels == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make(db.StringSlice, 0, len(labels))
	for _, l := range labels {
		lower := strings.ToLower(l)
		if _, ok := seen[lower]; !ok {
			seen[lower] = struct{}{}
			result = append(result, l)
		}
	}
	return result
}
