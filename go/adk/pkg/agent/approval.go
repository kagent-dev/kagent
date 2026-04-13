package agent

import (
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

// stripConfirmationPartsCallback is a BeforeModelCallback that removes
// adk_request_confirmation FunctionCall and FunctionResponse parts from the
// LLM request before it reaches any model provider. These are synthetic ADK
// HITL events the LLM never produced and does not need to reason about.
// The session still stores them so ADK's resume machinery can find them.
func MakeStripConfirmationPartsCallback() llmagent.BeforeModelCallback {
	return func(_ agent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
		out := req.Contents[:0]
		for _, c := range req.Contents {
			if c == nil {
				continue
			}
			filtered := c.Parts[:0]
			for _, p := range c.Parts {
				if p == nil {
					continue
				}
				if p.FunctionCall != nil && p.FunctionCall.Name == "adk_request_confirmation" {
					continue
				}
				if p.FunctionResponse != nil && p.FunctionResponse.Name == "adk_request_confirmation" {
					continue
				}
				filtered = append(filtered, p)
			}
			if len(filtered) == 0 {
				continue
			}
			c.Parts = filtered
			out = append(out, c)
		}
		req.Contents = out
		return nil, nil
	}
}

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
