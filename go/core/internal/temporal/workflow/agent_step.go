/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/core/internal/compiler"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"
)

// AgentStepRequest is the input sent to the agent child workflow.
// Fields are compatible with the ADK's ExecutionRequest (same JSON tags)
// so the agent worker can deserialize it without importing go/core.
type AgentStepRequest struct {
	SessionID string `json:"sessionID"`
	AgentName string `json:"agentName"`
	Message   []byte `json:"message"`
}

// AgentStepResult is the output received from the agent child workflow.
// Fields are compatible with the ADK's ExecutionResult.
type AgentStepResult struct {
	SessionID string `json:"sessionID"`
	Status    string `json:"status"` // "completed", "rejected", "failed"
	Response  []byte `json:"response,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// buildAgentChildOptions creates ChildWorkflowOptions for an agent step.
func buildAgentChildOptions(parentWorkflowID, stepName, agentRef string) workflow.ChildWorkflowOptions {
	return workflow.ChildWorkflowOptions{
		WorkflowID:        fmt.Sprintf("%s:agent:%s", parentWorkflowID, stepName),
		TaskQueue:         agentRef,
		ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
	}
}

// buildAgentMessage constructs the A2A-compatible JSON message for the agent.
// The message contains the prompt and any additional inputs as context.
func buildAgentMessage(prompt string, inputs map[string]string) ([]byte, error) {
	msg := map[string]interface{}{
		"prompt": prompt,
	}
	if len(inputs) > 0 {
		msg["context"] = inputs
	}
	return json.Marshal(msg)
}

// executeAgentStep dispatches an agent step as a Temporal child workflow.
// It renders the prompt, builds the agent request, executes the child workflow,
// and maps the agent response to a JSON output.
func executeAgentStep(
	ctx workflow.Context,
	step compiler.ExecutionStep,
	prompt string,
	inputs map[string]string,
	plan *compiler.ExecutionPlan,
) (json.RawMessage, error) {
	// Build child workflow options.
	childOpts := buildAgentChildOptions(plan.WorkflowID, step.Name, step.AgentRef)
	childCtx := workflow.WithChildOptions(ctx, childOpts)

	// Build the agent message.
	message, err := buildAgentMessage(prompt, inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to build agent message: %w", err)
	}

	// Build execution request.
	req := &AgentStepRequest{
		SessionID: fmt.Sprintf("dag-%s-%s", plan.WorkflowID, step.Name),
		AgentName: step.AgentRef,
		Message:   message,
	}

	// Execute the child workflow targeting the agent's task queue.
	var result AgentStepResult
	err = workflow.ExecuteChildWorkflow(childCtx, "AgentExecutionWorkflow", req).Get(childCtx, &result)
	if err != nil {
		return nil, fmt.Errorf("agent %q failed: %w", step.AgentRef, err)
	}

	// Check agent-level failure.
	if result.Status == "failed" || result.Status == "rejected" {
		reason := result.Reason
		if reason == "" {
			reason = "agent returned status: " + result.Status
		}
		return nil, fmt.Errorf("agent %q %s: %s", step.AgentRef, result.Status, reason)
	}

	// Map the agent response to output.
	return mapAgentOutput(result.Response, step.AgentRef)
}

// mapAgentOutput converts the raw agent response bytes into a JSON output.
// If the response is valid JSON, it's returned as-is. Otherwise it's wrapped
// as {"response": "<text>"}.
func mapAgentOutput(response []byte, agentRef string) (json.RawMessage, error) {
	if len(response) == 0 {
		return json.RawMessage(`{}`), nil
	}

	// Check if response is already valid JSON object or array.
	if json.Valid(response) {
		var probe interface{}
		if err := json.Unmarshal(response, &probe); err == nil {
			// If it's a map or slice, return as-is.
			switch probe.(type) {
			case map[string]interface{}, []interface{}:
				return response, nil
			}
		}
	}

	// Wrap non-object responses as {"response": "..."}
	wrapped := map[string]string{"response": string(response)}
	out, err := json.Marshal(wrapped)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent %q response: %w", agentRef, err)
	}
	return out, nil
}
