package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/db"
	"gorm.io/gorm"
)

// JobFilter defines filters for listing jobs.
type JobFilter struct {
	Status *db.JobStatus
	Label  *string
}

// CreateJobRequest holds the data for creating a new cron job.
type CreateJobRequest struct {
	Name        string
	Description string
	Schedule    string
	Command     string
	Labels      []string
	Timeout     int
	MaxRetries  int
}

// UpdateJobRequest holds fields for updating an existing cron job.
type UpdateJobRequest struct {
	Name        *string
	Description *string
	Schedule    *string
	Command     *string
	Status      *db.JobStatus
	Labels      *[]string
	Timeout     *int
	MaxRetries  *int
}

// Broadcaster is an interface for broadcasting job change events.
type Broadcaster interface {
	Broadcast(event interface{})
}

// CronService provides CRUD operations for cron jobs.
type CronService struct {
	db          *gorm.DB
	broadcaster Broadcaster
}

// NewCronService creates a new CronService.
func NewCronService(db *gorm.DB, b Broadcaster) *CronService {
	return &CronService{db: db, broadcaster: b}
}

// ListJobs returns jobs matching the filter.
func (s *CronService) ListJobs(ctx context.Context, filter JobFilter) ([]*db.CronJob, error) {
	q := s.db.WithContext(ctx)

	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}

	var jobs []*db.CronJob
	if err := q.Order("id DESC").Find(&jobs).Error; err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	if filter.Label != nil {
		label := strings.ToLower(*filter.Label)
		filtered := make([]*db.CronJob, 0)
		for _, j := range jobs {
			for _, l := range j.Labels {
				if strings.ToLower(l) == label {
					filtered = append(filtered, j)
					break
				}
			}
		}
		jobs = filtered
	}

	return jobs, nil
}

// GetJob returns a job by ID with recent executions preloaded.
func (s *CronService) GetJob(ctx context.Context, id uint) (*db.CronJob, error) {
	var job db.CronJob
	if err := s.db.WithContext(ctx).Preload("Executions", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("id DESC").Limit(20)
	}).First(&job, id).Error; err != nil {
		return nil, fmt.Errorf("job %d not found: %w", id, err)
	}
	return &job, nil
}

// CreateJob creates a new cron job.
func (s *CronService) CreateJob(ctx context.Context, req CreateJobRequest) (*db.CronJob, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 300
	}

	job := &db.CronJob{
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		Command:     req.Command,
		Status:      db.StatusActive,
		Labels:      deduplicateLabels(req.Labels),
		Timeout:     timeout,
		MaxRetries:  req.MaxRetries,
	}

	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	s.broadcaster.Broadcast(job)
	return job, nil
}

// UpdateJob updates an existing cron job's fields.
func (s *CronService) UpdateJob(ctx context.Context, id uint, req UpdateJobRequest) (*db.CronJob, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		job.Name = *req.Name
	}
	if req.Description != nil {
		job.Description = *req.Description
	}
	if req.Schedule != nil {
		job.Schedule = *req.Schedule
	}
	if req.Command != nil {
		job.Command = *req.Command
	}
	if req.Status != nil {
		if !db.ValidStatus(*req.Status) {
			return nil, fmt.Errorf("invalid status %q: valid statuses are %v", *req.Status, db.StatusList)
		}
		job.Status = *req.Status
	}
	if req.Labels != nil {
		job.Labels = deduplicateLabels(*req.Labels)
	}
	if req.Timeout != nil {
		job.Timeout = *req.Timeout
	}
	if req.MaxRetries != nil {
		job.MaxRetries = *req.MaxRetries
	}

	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		return nil, fmt.Errorf("failed to update job %d: %w", id, err)
	}

	s.broadcaster.Broadcast(job)
	return job, nil
}

// ToggleJob toggles a job between Active and Paused status.
func (s *CronService) ToggleJob(ctx context.Context, id uint) (*db.CronJob, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}

	if job.Status == db.StatusActive {
		job.Status = db.StatusPaused
	} else {
		job.Status = db.StatusActive
	}

	if err := s.db.WithContext(ctx).Save(job).Error; err != nil {
		return nil, fmt.Errorf("failed to toggle job %d: %w", id, err)
	}

	s.broadcaster.Broadcast(job)
	return job, nil
}

// DeleteJob deletes a job and all its executions.
func (s *CronService) DeleteJob(ctx context.Context, id uint) error {
	if _, err := s.GetJob(ctx, id); err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Where("cron_job_id = ?", id).Delete(&db.Execution{}).Error; err != nil {
		return fmt.Errorf("failed to delete executions of job %d: %w", id, err)
	}

	if err := s.db.WithContext(ctx).Delete(&db.CronJob{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete job %d: %w", id, err)
	}

	s.broadcaster.Broadcast(nil)
	return nil
}

// StartExecution records the start of a job execution.
func (s *CronService) StartExecution(ctx context.Context, jobID uint) (*db.Execution, error) {
	exec := &db.Execution{
		CronJobID: jobID,
		Status:    db.ExecRunning,
		StartedAt: time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(exec).Error; err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	now := time.Now()
	s.db.WithContext(ctx).Model(&db.CronJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"last_run_at":     now,
		"last_run_status": db.ExecRunning,
	})

	s.broadcaster.Broadcast(exec)
	return exec, nil
}

// FinishExecution records the completion of a job execution.
func (s *CronService) FinishExecution(ctx context.Context, execID uint, output string, exitCode int) (*db.Execution, error) {
	var exec db.Execution
	if err := s.db.WithContext(ctx).First(&exec, execID).Error; err != nil {
		return nil, fmt.Errorf("execution %d not found: %w", execID, err)
	}

	now := time.Now()
	duration := now.Sub(exec.StartedAt).Seconds()
	status := db.ExecSuccess
	if exitCode != 0 {
		status = db.ExecFailed
	}

	exec.Status = status
	exec.Output = output
	exec.ExitCode = &exitCode
	exec.FinishedAt = &now
	exec.Duration = &duration

	if err := s.db.WithContext(ctx).Save(&exec).Error; err != nil {
		return nil, fmt.Errorf("failed to finish execution %d: %w", execID, err)
	}

	s.db.WithContext(ctx).Model(&db.CronJob{}).Where("id = ?", exec.CronJobID).Update("last_run_status", status)

	s.broadcaster.Broadcast(&exec)
	return &exec, nil
}

// ListExecutions returns recent executions for a job.
func (s *CronService) ListExecutions(ctx context.Context, jobID uint, limit int) ([]*db.Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	var execs []*db.Execution
	if err := s.db.WithContext(ctx).Where("cron_job_id = ?", jobID).Order("id DESC").Limit(limit).Find(&execs).Error; err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}
	return execs, nil
}

// GetExecution returns a single execution by ID.
func (s *CronService) GetExecution(ctx context.Context, id uint) (*db.Execution, error) {
	var exec db.Execution
	if err := s.db.WithContext(ctx).First(&exec, id).Error; err != nil {
		return nil, fmt.Errorf("execution %d not found: %w", id, err)
	}
	return &exec, nil
}

// GetAllJobs returns all jobs without filtering (for the board view).
func (s *CronService) GetAllJobs(ctx context.Context) ([]*db.CronJob, error) {
	var jobs []*db.CronJob
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&jobs).Error; err != nil {
		return nil, fmt.Errorf("failed to list all jobs: %w", err)
	}
	return jobs, nil
}

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
