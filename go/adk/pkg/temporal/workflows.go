package temporal

import (
	"go.temporal.io/sdk/workflow"
)

// AgentExecutionWorkflow orchestrates a single agent execution run.
// Each LLM turn and tool call is a separate activity for maximum durability.
//
// Flow:
//  1. Initialize session (activity)
//  2. Loop: LLM invoke -> tool execution -> append events
//  3. Handle HITL signals when approval required
//  4. Start child workflows for A2A agent invocations
//  5. Save task on completion
func AgentExecutionWorkflow(ctx workflow.Context, req *ExecutionRequest) (*ExecutionResult, error) {
	// TODO: implement in Step 4
	return &ExecutionResult{
		SessionID: req.SessionID,
		Status:    "completed",
	}, nil
}
