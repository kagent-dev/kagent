package temporal

import (
	"context"
	"fmt"

	enumspb "go.temporal.io/api/enums/v1"
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

// ExecuteAgent starts an AgentExecutionWorkflow and returns the workflow run handle.
func (c *Client) ExecuteAgent(ctx context.Context, req *ExecutionRequest, cfg TemporalConfig) (client.WorkflowRun, error) {
	workflowID := WorkflowIDForSession(req.AgentName, req.SessionID)
	taskQueue := TaskQueueForAgent(req.AgentName)

	opts := client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                taskQueue,
		WorkflowExecutionTimeout: cfg.WorkflowTimeout,
	}

	run, err := c.temporal.ExecuteWorkflow(ctx, opts, AgentExecutionWorkflow, req)
	if err != nil {
		return nil, fmt.Errorf("failed to start workflow %s: %w", workflowID, err)
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
