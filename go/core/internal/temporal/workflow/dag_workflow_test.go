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
	"context"
	"encoding/json"
	"testing"
	"time"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// stubActionActivity is a stub used for registering the activity with the test environment.
func stubActionActivity(_ context.Context, _ *ActionRequest) (*ActionResult, error) {
	return nil, nil
}

func TestDAGWorkflow(t *testing.T) {
	tests := []struct {
		name           string
		plan           *compiler.ExecutionPlan
		mockSetup      func(env *testsuite.TestWorkflowEnvironment)
		expectedStatus string
		expectedPhases map[string]string
		checkOutput    func(t *testing.T, result *DAGResult)
	}{
		{
			name: "linear DAG A->B->C executes in order",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-linear",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "noop"},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"a"}},
					{Name: "c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"b"}},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.Anything).Return(
					&ActionResult{Output: json.RawMessage(`{"ok":true}`)}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"a": "Succeeded",
				"b": "Succeeded",
				"c": "Succeeded",
			},
		},
		{
			name: "parallel DAG A->[B,C]->D",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-parallel",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "noop"},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"a"}},
					{Name: "c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"a"}},
					{Name: "d", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"b", "c"}},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.Anything).Return(
					&ActionResult{Output: json.RawMessage(`{"ok":true}`)}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"a": "Succeeded",
				"b": "Succeeded",
				"c": "Succeeded",
				"d": "Succeeded",
			},
		},
		{
			name: "fail-fast: B fails with stop, C skipped",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-failfast",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "noop"},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "fail-action", DependsOn: []string{"a"}, OnFailure: "stop"},
					{Name: "c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"b"}},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.MatchedBy(func(req *ActionRequest) bool {
					return req.Action == "noop"
				})).Return(&ActionResult{Output: json.RawMessage(`{"ok":true}`)}, nil)

				env.OnActivity("ActionActivity", mock.Anything, mock.MatchedBy(func(req *ActionRequest) bool {
					return req.Action == "fail-action"
				})).Return(&ActionResult{Error: "something went wrong"}, nil)
			},
			expectedStatus: "failed",
			expectedPhases: map[string]string{
				"a": "Succeeded",
				"b": "Failed",
				"c": "Skipped",
			},
		},
		{
			name: "continue-on-error: B fails with continue, C still runs",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-continue",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "noop"},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "fail-action", DependsOn: []string{"a"}, OnFailure: "continue"},
					{Name: "c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"b"}},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.MatchedBy(func(req *ActionRequest) bool {
					return req.Action == "noop"
				})).Return(&ActionResult{Output: json.RawMessage(`{"ok":true}`)}, nil)

				env.OnActivity("ActionActivity", mock.Anything, mock.MatchedBy(func(req *ActionRequest) bool {
					return req.Action == "fail-action"
				})).Return(&ActionResult{Error: "something went wrong"}, nil)
			},
			expectedStatus: "failed",
			expectedPhases: map[string]string{
				"a": "Succeeded",
				"b": "Failed",
				"c": "Succeeded",
			},
		},
		{
			name: "context data flow: A output available to B",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-context",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{"base": "http://example.com"},
				Steps: []compiler.ExecutionStep{
					{
						Name:   "fetch",
						Type:   v1alpha2.StepTypeAction,
						Action: "http.request",
						With:   map[string]string{"url": "${{ params.base }}/api"},
						Output: &v1alpha2.StepOutput{
							Keys: map[string]string{"data_path": "path"},
						},
					},
					{
						Name:      "process",
						Type:      v1alpha2.StepTypeAction,
						Action:    "noop",
						DependsOn: []string{"fetch"},
						With:      map[string]string{"input": "${{ context.fetch.path }}"},
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.MatchedBy(func(req *ActionRequest) bool {
					return req.Action == "http.request"
				})).Return(&ActionResult{Output: json.RawMessage(`{"path":"/src","status":"ok"}`)}, nil)

				env.OnActivity("ActionActivity", mock.Anything, mock.MatchedBy(func(req *ActionRequest) bool {
					return req.Action == "noop"
				})).Return(func(ctx context.Context, req *ActionRequest) (*ActionResult, error) {
					// Echo inputs as output to verify context resolution.
					inputJSON, _ := json.Marshal(req.Inputs)
					return &ActionResult{Output: inputJSON}, nil
				})
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"fetch":   "Succeeded",
				"process": "Succeeded",
			},
			checkOutput: func(t *testing.T, result *DAGResult) {
				// Verify globals were populated from output.keys.
				require.Equal(t, "/src", result.Output["data_path"])
			},
		},
		{
			name: "single step with no dependencies",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-single",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{Name: "only", Type: v1alpha2.StepTypeAction, Action: "noop"},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.Anything).Return(
					&ActionResult{Output: json.RawMessage(`{"result":"done"}`)}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"only": "Succeeded",
			},
		},
		{
			name: "nil plan returns error",
			plan: nil,
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
			},
			expectedStatus: "", // workflow errors
		},
		{
			name: "step with custom retry and timeout policy",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-policy",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{
						Name:   "with-policy",
						Type:   v1alpha2.StepTypeAction,
						Action: "noop",
						Policy: &v1alpha2.StepPolicy{
							Retry: &v1alpha2.WorkflowRetryPolicy{
								MaxAttempts:        5,
								InitialInterval:    metav1.Duration{Duration: 2 * time.Second},
								BackoffCoefficient: "3.0",
							},
							Timeout: &v1alpha2.WorkflowTimeoutPolicy{
								StartToClose: metav1.Duration{Duration: 10 * time.Minute},
							},
						},
					},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.Anything).Return(
					&ActionResult{Output: json.RawMessage(`{"ok":true}`)}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"with-policy": "Succeeded",
			},
		},
		{
			name: "diamond DAG with output alias",
			plan: &compiler.ExecutionPlan{
				WorkflowID: "wf-test-diamond",
				TaskQueue:  "kagent-workflows",
				Params:     map[string]string{},
				Steps: []compiler.ExecutionStep{
					{
						Name:   "start",
						Type:   v1alpha2.StepTypeAction,
						Action: "noop",
						Output: &v1alpha2.StepOutput{As: "init"},
					},
					{Name: "left", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"start"}},
					{Name: "right", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"start"}},
					{Name: "join", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"left", "right"}},
				},
			},
			mockSetup: func(env *testsuite.TestWorkflowEnvironment) {
				env.OnActivity("ActionActivity", mock.Anything, mock.Anything).Return(
					&ActionResult{Output: json.RawMessage(`{"ok":true}`)}, nil,
				)
			},
			expectedStatus: "succeeded",
			expectedPhases: map[string]string{
				"start": "Succeeded",
				"left":  "Succeeded",
				"right": "Succeeded",
				"join":  "Succeeded",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(stubActionActivity, activity.RegisterOptions{Name: "ActionActivity"})

			tt.mockSetup(env)

			env.ExecuteWorkflow(DAGWorkflow, tt.plan)

			if tt.plan == nil {
				require.True(t, env.IsWorkflowCompleted())
				require.Error(t, env.GetWorkflowError())
				return
			}

			require.True(t, env.IsWorkflowCompleted())

			err := env.GetWorkflowError()
			require.NoError(t, err)

			var result DAGResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, tt.expectedStatus, result.Status)

			if tt.expectedPhases != nil {
				for _, sr := range result.Steps {
					expected, ok := tt.expectedPhases[sr.Name]
					if ok {
						require.Equal(t, expected, sr.Phase, "step %q phase mismatch", sr.Name)
					}
				}
			}

			if tt.checkOutput != nil {
				tt.checkOutput(t, &result)
			}
		})
	}
}

func TestBuildActivityOptions(t *testing.T) {
	tests := []struct {
		name   string
		policy *v1alpha2.StepPolicy
		check  func(t *testing.T, opts workflow.ActivityOptions)
	}{
		{
			name:   "nil policy uses defaults",
			policy: nil,
			check: func(t *testing.T, opts workflow.ActivityOptions) {
				require.Equal(t, defaultStartToClose, opts.StartToCloseTimeout)
				require.Nil(t, opts.RetryPolicy)
			},
		},
		{
			name: "custom timeout",
			policy: &v1alpha2.StepPolicy{
				Timeout: &v1alpha2.WorkflowTimeoutPolicy{
					StartToClose: metav1.Duration{Duration: 10 * time.Minute},
				},
			},
			check: func(t *testing.T, opts workflow.ActivityOptions) {
				require.Equal(t, 10*time.Minute, opts.StartToCloseTimeout)
			},
		},
		{
			name: "custom retry",
			policy: &v1alpha2.StepPolicy{
				Retry: &v1alpha2.WorkflowRetryPolicy{
					MaxAttempts:        5,
					InitialInterval:    metav1.Duration{Duration: 2 * time.Second},
					BackoffCoefficient: "3.0",
				},
			},
			check: func(t *testing.T, opts workflow.ActivityOptions) {
				require.NotNil(t, opts.RetryPolicy)
				require.Equal(t, int32(5), opts.RetryPolicy.MaximumAttempts)
				require.Equal(t, 2*time.Second, opts.RetryPolicy.InitialInterval)
				require.Equal(t, 3.0, opts.RetryPolicy.BackoffCoefficient)
			},
		},
		{
			name: "schedule-to-close and heartbeat",
			policy: &v1alpha2.StepPolicy{
				Timeout: &v1alpha2.WorkflowTimeoutPolicy{
					StartToClose:    metav1.Duration{Duration: 5 * time.Minute},
					ScheduleToClose: &metav1.Duration{Duration: 30 * time.Minute},
					Heartbeat:       &metav1.Duration{Duration: 1 * time.Minute},
				},
			},
			check: func(t *testing.T, opts workflow.ActivityOptions) {
				require.Equal(t, 5*time.Minute, opts.StartToCloseTimeout)
				require.Equal(t, 30*time.Minute, opts.ScheduleToCloseTimeout)
				require.Equal(t, 1*time.Minute, opts.HeartbeatTimeout)
			},
		},
		{
			name: "non-retryable error types",
			policy: &v1alpha2.StepPolicy{
				Retry: &v1alpha2.WorkflowRetryPolicy{
					MaxAttempts:        3,
					NonRetryableErrors: []string{"InvalidInput", "AuthFailed"},
				},
			},
			check: func(t *testing.T, opts workflow.ActivityOptions) {
				require.NotNil(t, opts.RetryPolicy)
				require.Equal(t, []string{"InvalidInput", "AuthFailed"}, opts.RetryPolicy.NonRetryableErrorTypes)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := buildActivityOptions(tt.policy)
			tt.check(t, opts)
		})
	}
}
