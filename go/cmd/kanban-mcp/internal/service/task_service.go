package service

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/db"
	"gorm.io/gorm"
)

// TaskFilter defines filters for listing tasks.
type TaskFilter struct {
	Status   *db.TaskStatus
	Assignee *string
	ParentID *uint // nil = top-level only (WHERE parent_id IS NULL)
}

// CreateTaskRequest holds the data for creating a new task.
type CreateTaskRequest struct {
	Title       string
	Description string
	Status      db.TaskStatus // defaults to StatusInbox if empty
}

// UpdateTaskRequest holds fields for updating an existing task.
type UpdateTaskRequest struct {
	Title           *string
	Description     *string
	Status          *db.TaskStatus
	Assignee        *string
	UserInputNeeded *bool
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
	if err := q.Preload("Subtasks").Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return tasks, nil
}

// GetTask returns a task by ID with its subtasks preloaded.
// Returns a wrapped gorm.ErrRecordNotFound if the task does not exist.
func (s *TaskService) GetTask(ctx context.Context, id uint) (*db.Task, error) {
	var task db.Task
	if err := s.db.WithContext(ctx).Preload("Subtasks").First(&task, id).Error; err != nil {
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
		ParentID:    &parentID,
	}

	if err := s.db.WithContext(ctx).Create(task).Error; err != nil {
		return nil, fmt.Errorf("failed to create subtask: %w", err)
	}

	s.broadcaster.Broadcast(task)
	return task, nil
}

// DeleteTask deletes a task and all its subtasks.
func (s *TaskService) DeleteTask(ctx context.Context, id uint) error {
	if _, err := s.GetTask(ctx, id); err != nil {
		return err
	}

	// Delete subtasks first (explicit cascade for SQLite + Postgres compatibility)
	if err := s.db.WithContext(ctx).Where("parent_id = ?", id).Delete(&db.Task{}).Error; err != nil {
		return fmt.Errorf("failed to delete subtasks of task %d: %w", id, err)
	}

	if err := s.db.WithContext(ctx).Delete(&db.Task{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete task %d: %w", id, err)
	}

	s.broadcaster.Broadcast(nil)
	return nil
}
