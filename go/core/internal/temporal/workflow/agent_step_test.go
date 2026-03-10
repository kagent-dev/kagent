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
	"testing"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// stubAgentWorkflow is a stub for registering the child workflow in the test env.
func stubAgentWorkflow(_ workflow.Context, _ *AgentStepRequest) (*AgentStepResult, error) {
	return nil, nil
}

func TestAgentStepInDAGWorkflow(t *testing.T) {
	tests := []struct {
		name           string
		plan           *compiler.ExecutionPlan
		mockSetup      func(env *testsuite.TestWorkflowEnvironment)
		expectedStatus string
		expectedPhases map[string]string
		checkOutput    func(t *testing.T, result *DAGResult)
	}{
		{
			name: "agent step with successful response",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-agent",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{"topic": "testing"},
				Steps: []compiler.ExecutionStep{
					{
						Name:     "analyze",
						Type:     v1alpha2.StepTypeAgent,
						AgentRef: "my-agent",
						Prompt:   "Analyze ${{ params.topic }}",
						Output: &v1alpha2.StepOutput{
							Keys: map[string]string{"summary": "summary"},
						},
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnWorkflow("AgentExecutionWorkflow", mock.Anything, mock.Anything).Return(
					&AgentStepResult{
						SessionID: "dag-wf-test-agent-analyze",
						Status:    "completed",
						Response:  []byte(`{"summary":"all good","details":"no issues found"}`),
					}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"analyze": "Succeeded",
			},
			checkOutput: func(t *testing.T, result *DAGResult) {
				require.Equal(t, "all good", result.Output["summary"])
			},
		},
		{
			name: "agent step with failed status",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-agent-fail",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{
						Name:     "failing-agent",
						Type:     v1alpha2.StepTypeAgent,
						AgentRef: "bad-agent",
						Prompt:   "do something",
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnWorkflow("AgentExecutionWorkflow", mock.Anything, mock.Anything).Return(
					&AgentStepResult{
						Status: "failed",
						Reason: "agent crashed",
					}, nil,
				)
			},
			expectedStatus: "failed",
			expectedPhases: map[string]string{
				"failing-agent": "Failed",
			},
		},
		{
			name: "agent step with rejected status",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-agent-rejected",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{
						Name:     "rejected-agent",
						Type:     v1alpha2.StepTypeAgent,
						AgentRef: "strict-agent",
						Prompt:   "invalid request",
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnWorkflow("AgentExecutionWorkflow", mock.Anything, mock.Anything).Return(
					&AgentStepResult{
						Status: "rejected",
						Reason: "invalid input format",
					}, nil,
				)
			},
			expectedStatus: "failed",
			expectedPhases: map[string]string{
				"rejected-agent": "Failed",
			},
		},
		{
			name: "agent step prompt rendered with context from prior step",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-agent-ctx",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{
						Name:   "fetch",
						Type:   v1alpha2.StepTypeAction,
						Action: "noop",
					},
					{
						Name:      "agent-step",
						Type:      v1alpha2.StepTypeAgent,
						AgentRef:  "analyzer",
						Prompt:    "Analyze the data",
						DependsOn: []string{"fetch"},
						Output: &v1alpha2.StepOutput{
							As: "analysis",
						},
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.Anything).Return(
					&ActionResult{Output: json.RawMessage(`{"data":"hello"}`)}, nil,
				)
				env.OnWorkflow("AgentExecutionWorkflow", mock.Anything, mock.Anything).Return(
					&AgentStepResult{
						Status:   "completed",
						Response: []byte(`{"result":"analyzed"}`),
					}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"fetch":      "Succeeded",
				"agent-step": "Succeeded",
			},
		},
		{
			name: "agent step with empty response",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-agent-empty",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{
						Name:     "empty-agent",
						Type:     v1alpha2.StepTypeAgent,
						AgentRef: "silent-agent",
						Prompt:   "do something quiet",
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnWorkflow("AgentExecutionWorkflow", mock.Anything, mock.Anything).Return(
					&AgentStepResult{
						Status:   "completed",
						Response: nil,
					}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"empty-agent": "Succeeded",
			},
		},
		{
			name: "agent step output keys mapping to globals",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-agent-keys",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{
						Name:     "keyed-agent",
						Type:     v1alpha2.StepTypeAgent,
						AgentRef: "data-agent",
						Prompt:   "extract info",
						Output: &v1alpha2.StepOutput{
							Keys: map[string]string{
								"extracted_name":  "name",
								"extracted_email": "email",
							},
						},
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnWorkflow("AgentExecutionWorkflow", mock.Anything, mock.Anything).Return(
					&AgentStepResult{
						Status:   "completed",
						Response: []byte(`{"name":"John","email":"john@example.com","extra":"ignored"}`),
					}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"keyed-agent": "Succeeded",
			},
			checkOutput: func(t *testing.T, result *DAGResult) {
				require.Equal(t, "John", result.Output["extracted_name"])
				require.Equal(t, "john@example.com", result.Output["extracted_email"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestWorkflowEnvironment()

			// Register stubs for activities and child workflows.
			env.RegisterActivityWithOptions(stubActionActivity, activity.RegisterOptions{Name: "ActionActivity"})
			env.RegisterWorkflowWithOptions(stubAgentWorkflow, workflow.RegisterOptions{Name: "AgentExecutionWorkflow"})

			tt.mockSetup(env)

			env.ExecuteWorkflow(DAGWorkflow, tt.plan)

			require.True(t, env.IsWorkflowCompleted())

			err := env.GetWorkflowError()
			require.NoError(t, err)

			var result DAGResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, tt.expectedStatus, result.Status)

			for _, sr := range result.Steps {
				expected, ok := tt.expectedPhases[sr.Name]
				if ok {
					require.Equal(t, expected, sr.Phase, "step %q phase mismatch", sr.Name)
				}
			}

			if tt.checkOutput != nil {
				tt.checkOutput(t, &result)
			}
		})
	}
}

func TestBuildAgentChildOptions(t *testing.T) {
	opts := buildAgentChildOptions("wf-ns-tpl-run", "analyze", "my-agent")
	require.Equal(t, "wf-ns-tpl-run:agent:analyze", opts.WorkflowID)
	require.Equal(t, "my-agent", opts.TaskQueue)
}

func TestBuildAgentMessage(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		inputs map[string]string
		check  func(t *testing.T, msg []byte)
	}{
		{
			name:   "prompt only",
			prompt: "Hello world",
			inputs: nil,
			check: func(t *testing.T, msg []byte) {
				var m map[string]interface{}
				require.NoError(t, json.Unmarshal(msg, &m))
				require.Equal(t, "Hello world", m["prompt"])
				_, hasCtx := m["context"]
				require.False(t, hasCtx)
			},
		},
		{
			name:   "prompt with inputs",
			prompt: "Analyze this",
			inputs: map[string]string{"key": "value"},
			check: func(t *testing.T, msg []byte) {
				var m map[string]interface{}
				require.NoError(t, json.Unmarshal(msg, &m))
				require.Equal(t, "Analyze this", m["prompt"])
				ctx := m["context"].(map[string]interface{})
				require.Equal(t, "value", ctx["key"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := buildAgentMessage(tt.prompt, tt.inputs)
			require.NoError(t, err)
			tt.check(t, msg)
		})
	}
}

func TestMapAgentOutput(t *testing.T) {
	tests := []struct {
		name     string
		response []byte
		check    func(t *testing.T, output json.RawMessage)
	}{
		{
			name:     "nil response returns empty object",
			response: nil,
			check: func(t *testing.T, output json.RawMessage) {
				require.JSONEq(t, `{}`, string(output))
			},
		},
		{
			name:     "json object passed through",
			response: []byte(`{"key":"value"}`),
			check: func(t *testing.T, output json.RawMessage) {
				require.JSONEq(t, `{"key":"value"}`, string(output))
			},
		},
		{
			name:     "json array passed through",
			response: []byte(`[1,2,3]`),
			check: func(t *testing.T, output json.RawMessage) {
				require.JSONEq(t, `[1,2,3]`, string(output))
			},
		},
		{
			name:     "plain string wrapped",
			response: []byte(`"hello"`),
			check: func(t *testing.T, output json.RawMessage) {
				require.JSONEq(t, `{"response":"\"hello\""}`, string(output))
			},
		},
		{
			name:     "non-json text wrapped",
			response: []byte(`some plain text`),
			check: func(t *testing.T, output json.RawMessage) {
				require.JSONEq(t, `{"response":"some plain text"}`, string(output))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := mapAgentOutput(tt.response, "test-agent")
			require.NoError(t, err)
			tt.check(t, output)
		})
	}
}

