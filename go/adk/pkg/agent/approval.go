package agent

import (
	"fmt"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
)

// MakeApprovalCallback creates a BeforeToolCallback that gates execution of
// tools in the approval set behind request_confirmation / ToolConfirmation.
// Port of kagent-adk/src/kagent/adk/_approval.py:make_approval_callback().
func MakeApprovalCallback(toolsRequiringApproval map[string]bool) llmagent.BeforeToolCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		toolName := t.Name()

		// No approval needed for this tool.
		if !toolsRequiringApproval[toolName] {
			return nil, nil
		}

		// On re-invocation after confirmation, ADK populates ToolConfirmation.
		if confirmation := ctx.ToolConfirmation(); confirmation != nil {
			if confirmation.Confirmed {
				// Approved — proceed with tool execution.
				return nil, nil
			}
			// Rejected — extract optional rejection reason from payload.
			payload, _ := confirmation.Payload.(map[string]any)
			reason, _ := payload["rejection_reason"].(string)
			if reason != "" {
				return map[string]any{
					"result": fmt.Sprintf("Tool call was rejected by user. Reason: %s", reason),
				}, nil
			}
			return map[string]any{
				"result": "Tool call was rejected by user.",
			}, nil
		}

		// First invocation — request confirmation and block execution.
		if err := ctx.RequestConfirmation(
			fmt.Sprintf("Tool '%s' requires approval before execution.", toolName),
			nil,
		); err != nil {
			return nil, fmt.Errorf("failed to request confirmation for tool %s: %w", toolName, err)
		}
		return map[string]any{
			"status": "confirmation_requested",
			"tool":   toolName,
		}, nil
	}
}
