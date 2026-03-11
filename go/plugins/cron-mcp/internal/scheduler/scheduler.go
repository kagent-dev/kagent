package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/service"
	"github.com/robfig/cron/v3"
)

// Scheduler manages cron job scheduling and execution.
type Scheduler struct {
	svc   *service.CronService
	cron  *cron.Cron
	shell string
	mu    sync.Mutex
	// maps job ID → cron entry ID so we can remove/update
	entries map[uint]cron.EntryID
}

// New creates a new Scheduler.
func New(svc *service.CronService, shell string) *Scheduler {
	return &Scheduler{
		svc:     svc,
		cron:    cron.New(cron.WithSeconds()),
		shell:   shell,
		entries: make(map[uint]cron.EntryID),
	}
}

// Start loads all active jobs from the database and starts the scheduler.
func (s *Scheduler) Start(ctx context.Context) error {
	jobs, err := s.svc.ListJobs(ctx, service.JobFilter{})
	if err != nil {
		return fmt.Errorf("failed to load jobs: %w", err)
	}

	for _, job := range jobs {
		if job.Status == db.StatusActive {
			s.AddJob(job)
		}
	}

	s.cron.Start()
	return nil
}

// Stop stops the cron scheduler.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// AddJob registers a job with the cron scheduler.
func (s *Scheduler) AddJob(job *db.CronJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry if any
	if entryID, ok := s.entries[job.ID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, job.ID)
	}

	if job.Status != db.StatusActive {
		return
	}

	jobID := job.ID
	command := job.Command
	timeout := job.Timeout
	if timeout <= 0 {
		timeout = 300
	}

	entryID, err := s.cron.AddFunc(job.Schedule, func() {
		s.executeJob(jobID, command, timeout)
	})
	if err != nil {
		log.Printf("failed to schedule job %d (%s): %v", job.ID, job.Schedule, err)
		return
	}

	s.entries[job.ID] = entryID

	// Update next run time
	entry := s.cron.Entry(entryID)
	if !entry.Next.IsZero() {
		next := entry.Next
		ctx := context.Background()
		s.svc.UpdateJob(ctx, job.ID, service.UpdateJobRequest{}) //nolint:errcheck
		// Direct DB update for next_run_at
		s.updateNextRun(job.ID, &next)
	}
}

// RemoveJob removes a job from the scheduler.
func (s *Scheduler) RemoveJob(jobID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[jobID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, jobID)
	}
}

// RunNow manually triggers a job execution.
func (s *Scheduler) RunNow(jobID uint, command string, timeout int) {
	go s.executeJob(jobID, command, timeout)
}

func (s *Scheduler) executeJob(jobID uint, command string, timeout int) {
	ctx := context.Background()

	execution, err := s.svc.StartExecution(ctx, jobID)
	if err != nil {
		log.Printf("failed to start execution for job %d: %v", jobID, err)
		return
	}

	timeoutDur := time.Duration(timeout) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeoutDur)
	defer cancel()

	cmd := exec.CommandContext(execCtx, s.shell, "-c", command)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			outBuf.WriteString("\n" + err.Error())
		}
	}

	// Truncate output to 64KB
	output := outBuf.String()
	if len(output) > 65536 {
		output = output[:65536] + "\n... (truncated)"
	}

	if _, err := s.svc.FinishExecution(ctx, execution.ID, output, exitCode); err != nil {
		log.Printf("failed to finish execution %d: %v", execution.ID, err)
	}

	// Update next run time
	s.mu.Lock()
	if entryID, ok := s.entries[jobID]; ok {
		entry := s.cron.Entry(entryID)
		if !entry.Next.IsZero() {
			next := entry.Next
			s.updateNextRun(jobID, &next)
		}
	}
	s.mu.Unlock()
}

func (s *Scheduler) updateNextRun(jobID uint, next *time.Time) {
	// Use a raw update to set next_run_at without triggering full model save
	ctx := context.Background()
	job, err := s.svc.GetJob(ctx, jobID)
	if err != nil {
		return
	}
	job.NextRunAt = next
	// We broadcast to update the UI
	s.svc.UpdateJob(ctx, jobID, service.UpdateJobRequest{}) //nolint:errcheck
}
