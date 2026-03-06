package temporal

import (
	"context"
	"fmt"

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

// Close closes the underlying Temporal client connection.
func (c *Client) Close() {
	c.temporal.Close()
}
