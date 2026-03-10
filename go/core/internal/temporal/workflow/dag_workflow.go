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
	"strconv"
	"sync"
	"time"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// DAGStatusQueryType is the query name for retrieving step statuses.
	DAGStatusQueryType = "dag-status"

	// defaultStartToClose is the default activity timeout.
	defaultStartToClose = 5 * time.Minute
)

// DAGResult holds the overall result of a DAG workflow execution.
type DAGResult struct {
	Status string            `json:"status"` // "succeeded" or "failed"
	Steps  []StepResult      `json:"steps"`
	Output map[string]string `json:"output,omitempty"`
}

// StepResult holds the execution result of a single step.
type StepResult struct {
	Name    string          `json:"name"`
	Phase   string          `json:"phase"`
	Output  json.RawMessage `json:"output,omitempty"`
	Error   string          `json:"error,omitempty"`
	Retries int32           `json:"retries,omitempty"`
}

// ActionRequest is the input to the ActionActivity.
type ActionRequest struct {
	Action string            `json:"action"`
	Inputs map[string]string `json:"inputs"`
}

// ActionResult is the output of the ActionActivity.
type ActionResult struct {
	Output json.RawMessage `json:"output"`
	Error  string          `json:"error,omitempty"`
}

// DAGWorkflow is the generic interpreter that executes an ExecutionPlan as a Temporal workflow.
func DAGWorkflow(ctx workflow.Context, plan *compiler.ExecutionPlan) (*DAGResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("execution plan is nil")
	}

	// Build workflow context for expression resolution.
	wfCtx := &compiler.WorkflowContext{
		StepOutputs: make(map[string]json.RawMessage),
		Globals:     make(map[string]string),
	}
	// Extract workflow metadata from plan ID (format: wf-{namespace}-{template}-{run}).
	wfCtx.WorkflowRunName = plan.WorkflowID

	// Thread-safe state for step tracking.
	var mu sync.Mutex
	completed := make(map[string]bool)
	failed := make(map[string]bool)
	stepPhases := make(map[string]string)
	stepResults := make(map[string]*StepResult)

	// Initialize all steps as Pending.
	for _, step := range plan.Steps {
		stepPhases[step.Name] = string(v1alpha2.StepPhasePending)
		stepResults[step.Name] = &StepResult{
			Name:  step.Name,
			Phase: string(v1alpha2.StepPhasePending),
		}
	}

	// Register query handler for status syncer.
	if err := workflow.SetQueryHandler(ctx, DAGStatusQueryType, func() ([]StepResult, error) {
		mu.Lock()
		defer mu.Unlock()
		results := make([]StepResult, 0, len(plan.Steps))
		for _, step := range plan.Steps {
			results = append(results, *stepResults[step.Name])
		}
		return results, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to register query handler: %w", err)
	}

	// Result channel: each step goroutine sends its result here.
	resultCh := workflow.NewChannel(ctx)

	// Launch one goroutine per step.
	for _, step := range plan.Steps {
		step := step // capture loop variable
		workflow.Go(ctx, func(gCtx workflow.Context) {
			result := executeStep(gCtx, step, plan, wfCtx, &mu, completed, failed, stepPhases)

			mu.Lock()
			stepResults[step.Name] = result
			mu.Unlock()

			resultCh.Send(gCtx, result)
		})
	}

	// Collect results from all steps.
	allResults := make([]StepResult, 0, len(plan.Steps))
	for range plan.Steps {
		var result StepResult
		resultCh.Receive(ctx, &result)
		allResults = append(allResults, result)
	}

	// Determine overall status.
	overallStatus := "succeeded"
	for _, r := range allResults {
		if r.Phase == string(v1alpha2.StepPhaseFailed) {
			overallStatus = "failed"
			break
		}
	}

	// Build output from globals.
	mu.Lock()
	output := make(map[string]string, len(wfCtx.Globals))
	for k, v := range wfCtx.Globals {
		output[k] = v
	}
	mu.Unlock()

	return &DAGResult{
		Status: overallStatus,
		Steps:  allResults,
		Output: output,
	}, nil
}

// executeStep runs a single step after waiting for its dependencies.
func executeStep(
	ctx workflow.Context,
	step compiler.ExecutionStep,
	plan *compiler.ExecutionPlan,
	wfCtx *compiler.WorkflowContext,
	mu *sync.Mutex,
	completed, failed map[string]bool,
	stepPhases map[string]string,
) *StepResult {
	result := &StepResult{Name: step.Name}

	// Wait for all dependencies to complete.
	if len(step.DependsOn) > 0 {
		_ = workflow.Await(ctx, func() bool {
			mu.Lock()
			defer mu.Unlock()
			for _, dep := range step.DependsOn {
				if !completed[dep] && !failed[dep] {
					return false
				}
			}
			return true
		})
	}

	// Check if we should skip due to failed stop-mode dependencies.
	mu.Lock()
	shouldSkip := false
	for _, dep := range step.DependsOn {
		if failed[dep] {
			// Find dep step to check onFailure mode.
			for _, s := range plan.Steps {
				if s.Name == dep {
					onFailure := s.OnFailure
					if onFailure == "" {
						onFailure = "stop"
					}
					if onFailure == "stop" {
						shouldSkip = true
					}
					break
				}
			}
		}
	}
	if shouldSkip {
		stepPhases[step.Name] = string(v1alpha2.StepPhaseSkipped)
		completed[step.Name] = true
		mu.Unlock()
		result.Phase = string(v1alpha2.StepPhaseSkipped)
		result.Error = "skipped: dependency failed"
		return result
	}

	// Mark as running.
	stepPhases[step.Name] = string(v1alpha2.StepPhaseRunning)
	mu.Unlock()

	// Resolve expressions in step inputs.
	mu.Lock()
	resolvedInputs, err := resolveStepInputs(step, plan.Params, wfCtx)
	mu.Unlock()
	if err != nil {
		mu.Lock()
		stepPhases[step.Name] = string(v1alpha2.StepPhaseFailed)
		failed[step.Name] = true
		mu.Unlock()
		result.Phase = string(v1alpha2.StepPhaseFailed)
		result.Error = fmt.Sprintf("expression resolution failed: %v", err)
		return result
	}

	// Configure activity options from step policy.
	actOpts := buildActivityOptions(step.Policy)
	actCtx := workflow.WithActivityOptions(ctx, actOpts)

	// Dispatch based on step type.
	var output json.RawMessage
	switch step.Type {
	case v1alpha2.StepTypeAction:
		output, err = executeActionStep(actCtx, step.Action, resolvedInputs)
	case v1alpha2.StepTypeAgent:
		mu.Lock()
		resolvedPrompt, promptErr := compiler.ResolveExpression(step.Prompt, plan.Params, wfCtx)
		mu.Unlock()
		if promptErr != nil {
			err = fmt.Errorf("prompt resolution failed: %w", promptErr)
		} else {
			output, err = executeAgentStep(ctx, step, resolvedPrompt, resolvedInputs, plan)
		}
	default:
		err = fmt.Errorf("unknown step type: %q", step.Type)
	}

	// Store results.
	mu.Lock()
	defer mu.Unlock()

	if err != nil {
		stepPhases[step.Name] = string(v1alpha2.StepPhaseFailed)
		failed[step.Name] = true
		result.Phase = string(v1alpha2.StepPhaseFailed)
		result.Error = err.Error()
	} else {
		stepPhases[step.Name] = string(v1alpha2.StepPhaseSucceeded)
		completed[step.Name] = true
		result.Phase = string(v1alpha2.StepPhaseSucceeded)
		result.Output = output

		// Store output in workflow context.
		storeStepOutput(step, output, wfCtx)
	}

	return result
}

// resolveStepInputs resolves all ${{ }} expressions in step input values.
// Caller must hold mu lock.
func resolveStepInputs(step compiler.ExecutionStep, params map[string]string, wfCtx *compiler.WorkflowContext) (map[string]string, error) {
	if len(step.With) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(step.With))
	for k, v := range step.With {
		val, err := compiler.ResolveExpression(v, params, wfCtx)
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", k, err)
		}
		resolved[k] = val
	}
	return resolved, nil
}

// storeStepOutput stores step output in the workflow context using the step's output configuration.
// Caller must hold mu lock.
func storeStepOutput(step compiler.ExecutionStep, output json.RawMessage, wfCtx *compiler.WorkflowContext) {
	if output == nil {
		return
	}

	// Store under alias or step name.
	key := step.Name
	if step.Output != nil && step.Output.As != "" {
		key = step.Output.As
	}
	wfCtx.StepOutputs[key] = output

	// Map selected keys to globals.
	if step.Output != nil && len(step.Output.Keys) > 0 {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(output, &obj); err == nil {
			for globalKey, fieldPath := range step.Output.Keys {
				if val, ok := obj[fieldPath]; ok {
					var s string
					if err := json.Unmarshal(val, &s); err == nil {
						wfCtx.Globals[globalKey] = s
					} else {
						wfCtx.Globals[globalKey] = string(val)
					}
				}
			}
		}
	}
}

// executeActionStep dispatches an action step to the ActionActivity.
func executeActionStep(ctx workflow.Context, action string, inputs map[string]string) (json.RawMessage, error) {
	req := &ActionRequest{
		Action: action,
		Inputs: inputs,
	}
	var result ActionResult
	err := workflow.ExecuteActivity(ctx, "ActionActivity", req).Get(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("action %q failed: %w", action, err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("action %q returned error: %s", action, result.Error)
	}
	return result.Output, nil
}

// buildActivityOptions creates Temporal ActivityOptions from a step policy.
func buildActivityOptions(policy *v1alpha2.StepPolicy) workflow.ActivityOptions {
	opts := workflow.ActivityOptions{
		StartToCloseTimeout: defaultStartToClose,
	}

	if policy == nil {
		return opts
	}

	if policy.Timeout != nil {
		if policy.Timeout.StartToClose.Duration > 0 {
			opts.StartToCloseTimeout = policy.Timeout.StartToClose.Duration
		}
		if policy.Timeout.ScheduleToClose != nil && policy.Timeout.ScheduleToClose.Duration > 0 {
			opts.ScheduleToCloseTimeout = policy.Timeout.ScheduleToClose.Duration
		}
		if policy.Timeout.Heartbeat != nil && policy.Timeout.Heartbeat.Duration > 0 {
			opts.HeartbeatTimeout = policy.Timeout.Heartbeat.Duration
		}
	}

	if policy.Retry != nil {
		retryPolicy := &temporal.RetryPolicy{}
		if policy.Retry.MaxAttempts > 0 {
			retryPolicy.MaximumAttempts = policy.Retry.MaxAttempts
		}
		if policy.Retry.InitialInterval.Duration > 0 {
			retryPolicy.InitialInterval = policy.Retry.InitialInterval.Duration
		}
		if policy.Retry.MaximumInterval.Duration > 0 {
			retryPolicy.MaximumInterval = policy.Retry.MaximumInterval.Duration
		}
		if policy.Retry.BackoffCoefficient != "" {
			if coeff, err := strconv.ParseFloat(policy.Retry.BackoffCoefficient, 64); err == nil {
				retryPolicy.BackoffCoefficient = coeff
			}
		}
		if len(policy.Retry.NonRetryableErrors) > 0 {
			retryPolicy.NonRetryableErrorTypes = policy.Retry.NonRetryableErrors
		}
		opts.RetryPolicy = retryPolicy
	}

	return opts
}
