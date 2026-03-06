package temporal

import "context"

// WorkflowClient defines the operations used by MCP tools, REST handlers, and SSE hub.
type WorkflowClient interface {
	ListWorkflows(ctx context.Context, filter WorkflowFilter) ([]*WorkflowSummary, error)
	GetWorkflow(ctx context.Context, workflowID string) (*WorkflowDetail, error)
	CancelWorkflow(ctx context.Context, workflowID string) error
	SignalWorkflow(ctx context.Context, workflowID, signalName string, data interface{}) error
}
