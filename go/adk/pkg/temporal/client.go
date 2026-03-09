package temporal

import (
	"context"
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

// Client wraps a Temporal client with agent-specific workflow operations.
type Client struct {
	temporal client.Client
}

// NewClient creates a new Temporal client connected to the given address.
func NewClient(cfg ClientConfig) (*Client, error) {
	c, err := client.Dial(client.Options{
		HostPort:  cfg.TemporalAddr,
		Namespace: cfg.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}
	return &Client{temporal: c}, nil
}

// NewClientFromExisting wraps an existing Temporal client.
func NewClientFromExisting(c client.Client) *Client {
	return &Client{temporal: c}
}

// ExecuteAgent sends a message to a session workflow using SignalWithStartWorkflow.
// If the workflow is already running, the message is delivered as a signal.
// If not, a new workflow is started and the message is delivered atomically.
// This ensures one workflow per session with multiple LLM invocations.
func (c *Client) ExecuteAgent(ctx context.Context, req *ExecutionRequest, cfg TemporalConfig) (client.WorkflowRun, error) {
	taskQueue := cfg.TaskQueue
	if taskQueue == "" {
		taskQueue = TaskQueueForAgent(req.AgentName)
	}
	workflowID := WorkflowIDForSession(taskQueue, req.SessionID)

	msg := MessageSignal{
		Message:     req.Message,
		NATSSubject: req.NATSSubject,
	}

	opts := client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                taskQueue,
		WorkflowExecutionTimeout: cfg.WorkflowTimeout,
	}

	run, err := c.temporal.SignalWithStartWorkflow(ctx, workflowID, MessageSignalName, msg, opts, AgentExecutionWorkflow, req)
	if err != nil {
		return nil, fmt.Errorf("failed to signal-with-start workflow %s: %w", workflowID, err)
	}
	return run, nil
}

// SignalApproval sends an HITL approval signal to a running workflow.
func (c *Client) SignalApproval(ctx context.Context, workflowID string, decision *ApprovalDecision) error {
	return c.temporal.SignalWorkflow(ctx, workflowID, "", ApprovalSignalName, decision)
}

// GetWorkflowStatus queries the current status of a workflow execution.
func (c *Client) GetWorkflowStatus(ctx context.Context, workflowID string) (*WorkflowStatus, error) {
	resp, err := c.temporal.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to describe workflow %s: %w", workflowID, err)
	}

	info := resp.GetWorkflowExecutionInfo()
	if info == nil {
		return nil, fmt.Errorf("no execution info for workflow %s", workflowID)
	}

	return &WorkflowStatus{
		WorkflowID: info.GetExecution().GetWorkflowId(),
		RunID:      info.GetExecution().GetRunId(),
		Status:     workflowStatusString(info.GetStatus()),
		TaskQueue:  info.GetTaskQueue(),
	}, nil
}

// WaitForResult blocks until the workflow completes and returns the result.
func (c *Client) WaitForResult(ctx context.Context, workflowID string) (*ExecutionResult, error) {
	run := c.temporal.GetWorkflow(ctx, workflowID, "")
	var result ExecutionResult
	if err := run.Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("workflow %s failed: %w", workflowID, err)
	}
	return &result, nil
}

// Temporal returns the underlying Temporal SDK client for worker creation.
func (c *Client) Temporal() client.Client {
	return c.temporal
}

// TerminateRunningWorkflows terminates all running workflows on the given task queue.
// This should be called on pod startup to clean up orphaned workflows from a previous
// pod lifecycle. Workflows mid-processing have no A2A executor waiting for their
// completion events, so they must be terminated to avoid hanging in "working" state.
func (c *Client) TerminateRunningWorkflows(ctx context.Context, taskQueue string) (int, error) {
	query := fmt.Sprintf("TaskQueue = %q AND ExecutionStatus = \"Running\"", taskQueue)

	terminated := 0
	var nextPageToken []byte

	for {
		resp, err := c.temporal.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
			Query:         query,
			NextPageToken: nextPageToken,
		})
		if err != nil {
			return terminated, fmt.Errorf("failed to list running workflows: %w", err)
		}

		for _, exec := range resp.GetExecutions() {
			wfID := exec.GetExecution().GetWorkflowId()
			runID := exec.GetExecution().GetRunId()
			err := c.temporal.TerminateWorkflow(ctx, wfID, runID, "agent pod restarted")
			if err != nil {
				// Log but continue — the workflow may have already completed.
				continue
			}
			terminated++
		}

		nextPageToken = resp.GetNextPageToken()
		if len(nextPageToken) == 0 {
			break
		}
	}

	return terminated, nil
}

// Close closes the underlying Temporal client connection.
func (c *Client) Close() {
	c.temporal.Close()
}

// workflowStatusString converts a Temporal WorkflowExecutionStatus enum to a human-readable string.
func workflowStatusString(status enumspb.WorkflowExecutionStatus) string {
	switch status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return "running"
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "timed_out"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "continued_as_new"
	default:
		return "unknown"
	}
}
