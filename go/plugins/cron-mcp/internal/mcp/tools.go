package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/scheduler"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Board is the response for get_board, grouping jobs by status.
type Board struct {
	Groups []Group `json:"groups"`
}

// Group holds jobs for a single status.
type Group struct {
	Status string        `json:"status"`
	Jobs   []*db.CronJob `json:"jobs"`
}

// NewServer creates and returns an MCP server with all cron tools registered.
func NewServer(svc *service.CronService, sched *scheduler.Scheduler) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "cron",
		Version: "v1.0.0",
	}, nil)

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_jobs",
		Description: "List cron jobs, optionally filtered by status or label.",
	}, handleListJobs(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_job",
		Description: "Get a cron job by ID including recent executions.",
	}, handleGetJob(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "create_job",
		Description: "Create a new cron job with a schedule (cron expression) and command.",
	}, handleCreateJob(svc, sched))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "update_job",
		Description: "Update cron job fields (name, description, schedule, command, status, labels, timeout, max_retries).",
	}, handleUpdateJob(svc, sched))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "toggle_job",
		Description: "Toggle a job between Active and Paused status.",
	}, handleToggleJob(svc, sched))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "delete_job",
		Description: "Delete a cron job and all its execution history.",
	}, handleDeleteJob(svc, sched))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "run_job",
		Description: "Manually trigger a cron job execution immediately.",
	}, handleRunJob(svc, sched))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_executions",
		Description: "List recent executions for a cron job.",
	}, handleListExecutions(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_execution",
		Description: "Get a single execution by ID with full output.",
	}, handleGetExecution(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "get_board",
		Description: "Get all cron jobs grouped by status.",
	}, handleGetBoard(svc))

	return server
}

func textResult(v interface{}) (*mcpsdk.CallToolResult, interface{}, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to marshal result: %v", err)), nil, nil
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: string(data)},
		},
	}, nil, nil
}

func errorResult(msg string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: msg},
		},
	}
}

// --- Tool input types ---

type listJobsInput struct {
	Status string `json:"status,omitempty"`
	Label  string `json:"label,omitempty"`
}

type getJobInput struct {
	ID uint `json:"id"`
}

type createJobInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Schedule    string   `json:"schedule"`
	Command     string   `json:"command"`
	Labels      []string `json:"labels,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
	MaxRetries  int      `json:"max_retries,omitempty"`
}

type updateJobInput struct {
	ID          uint      `json:"id"`
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Schedule    *string   `json:"schedule,omitempty"`
	Command     *string   `json:"command,omitempty"`
	Status      *string   `json:"status,omitempty"`
	Labels      *[]string `json:"labels,omitempty"`
	Timeout     *int      `json:"timeout,omitempty"`
	MaxRetries  *int      `json:"max_retries,omitempty"`
}

type toggleJobInput struct {
	ID uint `json:"id"`
}

type deleteJobInput struct {
	ID uint `json:"id"`
}

type runJobInput struct {
	ID uint `json:"id"`
}

type listExecutionsInput struct {
	JobID uint `json:"job_id"`
	Limit int  `json:"limit,omitempty"`
}

type getExecutionInput struct {
	ID uint `json:"id"`
}

// --- Tool handlers ---

func handleListJobs(svc *service.CronService) func(context.Context, *mcpsdk.CallToolRequest, listJobsInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input listJobsInput) (*mcpsdk.CallToolResult, interface{}, error) {
		filter := service.JobFilter{}
		if input.Status != "" {
			s := db.JobStatus(input.Status)
			filter.Status = &s
		}
		if input.Label != "" {
			filter.Label = &input.Label
		}
		jobs, err := svc.ListJobs(ctx, filter)
		if err != nil {
			return errorResult(fmt.Sprintf("list_jobs failed: %v", err)), nil, nil
		}
		return textResult(jobs)
	}
}

func handleGetJob(svc *service.CronService) func(context.Context, *mcpsdk.CallToolRequest, getJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input getJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
		job, err := svc.GetJob(ctx, input.ID)
		if err != nil {
			return errorResult(fmt.Sprintf("get_job failed: %v", err)), nil, nil
		}
		return textResult(job)
	}
}

func handleCreateJob(svc *service.CronService, sched *scheduler.Scheduler) func(context.Context, *mcpsdk.CallToolRequest, createJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input createJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
		req := service.CreateJobRequest{
			Name:        input.Name,
			Description: input.Description,
			Schedule:    input.Schedule,
			Command:     input.Command,
			Labels:      input.Labels,
			Timeout:     input.Timeout,
			MaxRetries:  input.MaxRetries,
		}
		job, err := svc.CreateJob(ctx, req)
		if err != nil {
			return errorResult(fmt.Sprintf("create_job failed: %v", err)), nil, nil
		}
		if sched != nil {
			sched.AddJob(job)
		}
		return textResult(job)
	}
}

func handleUpdateJob(svc *service.CronService, sched *scheduler.Scheduler) func(context.Context, *mcpsdk.CallToolRequest, updateJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input updateJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
		req := service.UpdateJobRequest{
			Name:        input.Name,
			Description: input.Description,
			Schedule:    input.Schedule,
			Command:     input.Command,
			Labels:      input.Labels,
			Timeout:     input.Timeout,
			MaxRetries:  input.MaxRetries,
		}
		if input.Status != nil {
			s := db.JobStatus(*input.Status)
			req.Status = &s
		}
		job, err := svc.UpdateJob(ctx, input.ID, req)
		if err != nil {
			return errorResult(fmt.Sprintf("update_job failed: %v", err)), nil, nil
		}
		if sched != nil {
			sched.AddJob(job)
		}
		return textResult(job)
	}
}

func handleToggleJob(svc *service.CronService, sched *scheduler.Scheduler) func(context.Context, *mcpsdk.CallToolRequest, toggleJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input toggleJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
		job, err := svc.ToggleJob(ctx, input.ID)
		if err != nil {
			return errorResult(fmt.Sprintf("toggle_job failed: %v", err)), nil, nil
		}
		if sched != nil {
			sched.AddJob(job)
		}
		return textResult(job)
	}
}

func handleDeleteJob(svc *service.CronService, sched *scheduler.Scheduler) func(context.Context, *mcpsdk.CallToolRequest, deleteJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input deleteJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
		if sched != nil {
			sched.RemoveJob(input.ID)
		}
		if err := svc.DeleteJob(ctx, input.ID); err != nil {
			return errorResult(fmt.Sprintf("delete_job failed: %v", err)), nil, nil
		}
		return textResult(map[string]interface{}{"deleted": true, "id": input.ID})
	}
}

func handleRunJob(svc *service.CronService, sched *scheduler.Scheduler) func(context.Context, *mcpsdk.CallToolRequest, runJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input runJobInput) (*mcpsdk.CallToolResult, interface{}, error) {
		job, err := svc.GetJob(ctx, input.ID)
		if err != nil {
			return errorResult(fmt.Sprintf("run_job failed: %v", err)), nil, nil
		}
		if sched != nil {
			sched.RunNow(job.ID, job.Command, job.Timeout)
		}
		return textResult(map[string]interface{}{"triggered": true, "id": input.ID})
	}
}

func handleListExecutions(svc *service.CronService) func(context.Context, *mcpsdk.CallToolRequest, listExecutionsInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input listExecutionsInput) (*mcpsdk.CallToolResult, interface{}, error) {
		execs, err := svc.ListExecutions(ctx, input.JobID, input.Limit)
		if err != nil {
			return errorResult(fmt.Sprintf("list_executions failed: %v", err)), nil, nil
		}
		return textResult(execs)
	}
}

func handleGetExecution(svc *service.CronService) func(context.Context, *mcpsdk.CallToolRequest, getExecutionInput) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input getExecutionInput) (*mcpsdk.CallToolResult, interface{}, error) {
		exec, err := svc.GetExecution(ctx, input.ID)
		if err != nil {
			return errorResult(fmt.Sprintf("get_execution failed: %v", err)), nil, nil
		}
		return textResult(exec)
	}
}

func handleGetBoard(svc *service.CronService) func(context.Context, *mcpsdk.CallToolRequest, interface{}) (*mcpsdk.CallToolResult, interface{}, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ interface{}) (*mcpsdk.CallToolResult, interface{}, error) {
		board, err := buildBoard(ctx, svc)
		if err != nil {
			return errorResult(fmt.Sprintf("get_board failed: %v", err)), nil, nil
		}
		return textResult(board)
	}
}

func buildBoard(ctx context.Context, svc *service.CronService) (*Board, error) {
	jobs, err := svc.GetAllJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	byStatus := make(map[db.JobStatus][]*db.CronJob)
	for _, j := range jobs {
		byStatus[j.Status] = append(byStatus[j.Status], j)
	}

	groups := make([]Group, 0, len(db.StatusList))
	for _, status := range db.StatusList {
		g := Group{
			Status: string(status),
			Jobs:   byStatus[status],
		}
		if g.Jobs == nil {
			g.Jobs = []*db.CronJob{}
		}
		groups = append(groups, g)
	}

	return &Board{Groups: groups}, nil
}
