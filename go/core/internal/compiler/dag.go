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

package compiler

import (
	"encoding/json"
	"fmt"
	"strconv"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
)

const maxStepCount = 200

// ExecutionPlan is the JSON-serializable input to the DAGWorkflow Temporal interpreter.
type ExecutionPlan struct {
	WorkflowID string            `json:"workflowID"`
	TaskQueue  string            `json:"taskQueue"`
	Params     map[string]string `json:"params"`
	Steps      []ExecutionStep   `json:"steps"`
	Defaults   *v1alpha2.StepPolicyDefaults `json:"defaults,omitempty"`
}

// ExecutionStep represents a single step in the execution plan with merged policies.
type ExecutionStep struct {
	Name      string              `json:"name"`
	Type      v1alpha2.StepType   `json:"type"`
	Action    string              `json:"action,omitempty"`
	AgentRef  string              `json:"agentRef,omitempty"`
	Prompt    string              `json:"prompt,omitempty"`
	With      map[string]string   `json:"with,omitempty"`
	DependsOn []string            `json:"dependsOn,omitempty"`
	Output    *v1alpha2.StepOutput `json:"output,omitempty"`
	Policy    *v1alpha2.StepPolicy `json:"policy,omitempty"`
	OnFailure string              `json:"onFailure,omitempty"`
}

// DAGCompiler validates WorkflowTemplateSpec and produces ExecutionPlans.
type DAGCompiler struct{}

// NewDAGCompiler creates a new DAGCompiler.
func NewDAGCompiler() *DAGCompiler {
	return &DAGCompiler{}
}

// Validate checks a WorkflowTemplateSpec for structural and semantic errors.
func (c *DAGCompiler) Validate(spec *v1alpha2.WorkflowTemplateSpec) error {
	if len(spec.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}
	if len(spec.Steps) > maxStepCount {
		return fmt.Errorf("workflow has %d steps, maximum is %d", len(spec.Steps), maxStepCount)
	}

	// Build step name index.
	stepNames := make(map[string]bool, len(spec.Steps))
	for _, s := range spec.Steps {
		if stepNames[s.Name] {
			return fmt.Errorf("duplicate step name: %q", s.Name)
		}
		stepNames[s.Name] = true
	}

	// Validate each step.
	for _, s := range spec.Steps {
		if err := validateStep(s, stepNames); err != nil {
			return fmt.Errorf("step %q: %w", s.Name, err)
		}
	}

	// Cycle detection via topological sort (Kahn's algorithm).
	if err := detectCycles(spec.Steps, stepNames); err != nil {
		return err
	}

	return nil
}

// Compile validates params and produces an ExecutionPlan ready for Temporal submission.
func (c *DAGCompiler) Compile(spec *v1alpha2.WorkflowTemplateSpec, params map[string]string, workflowID, taskQueue string) (*ExecutionPlan, error) {
	if err := c.Validate(spec); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	resolvedParams, err := resolveParams(spec.Params, params)
	if err != nil {
		return nil, fmt.Errorf("parameter resolution failed: %w", err)
	}

	steps := make([]ExecutionStep, 0, len(spec.Steps))
	for _, s := range spec.Steps {
		es := ExecutionStep{
			Name:      s.Name,
			Type:      s.Type,
			Action:    s.Action,
			AgentRef:  s.AgentRef,
			Prompt:    s.Prompt,
			With:      s.With,
			DependsOn: s.DependsOn,
			Output:    s.Output,
			OnFailure: s.OnFailure,
		}
		es.Policy = mergePolicies(s.Policy, spec.Defaults)
		steps = append(steps, es)
	}

	plan := &ExecutionPlan{
		WorkflowID: workflowID,
		TaskQueue:  taskQueue,
		Params:     resolvedParams,
		Steps:      steps,
		Defaults:   spec.Defaults,
	}

	// Verify the plan is JSON-serializable.
	if _, err := json.Marshal(plan); err != nil {
		return nil, fmt.Errorf("execution plan is not JSON-serializable: %w", err)
	}

	return plan, nil
}

// validateStep checks a single step for type-specific requirements and valid dependencies.
func validateStep(s v1alpha2.StepSpec, stepNames map[string]bool) error {
	switch s.Type {
	case v1alpha2.StepTypeAction:
		if s.Action == "" {
			return fmt.Errorf("action step must have 'action' field")
		}
	case v1alpha2.StepTypeAgent:
		if s.AgentRef == "" {
			return fmt.Errorf("agent step must have 'agentRef' field")
		}
	default:
		return fmt.Errorf("unknown step type: %q", s.Type)
	}

	for _, dep := range s.DependsOn {
		if !stepNames[dep] {
			return fmt.Errorf("depends on nonexistent step: %q", dep)
		}
		if dep == s.Name {
			return fmt.Errorf("step cannot depend on itself")
		}
	}

	return nil
}

// detectCycles uses Kahn's algorithm (topological sort) to detect cycles in the DAG.
func detectCycles(steps []v1alpha2.StepSpec, stepNames map[string]bool) error {
	// Build adjacency list and in-degree counts.
	inDegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))

	for _, s := range steps {
		if _, ok := inDegree[s.Name]; !ok {
			inDegree[s.Name] = 0
		}
		for _, dep := range s.DependsOn {
			dependents[dep] = append(dependents[dep], s.Name)
			inDegree[s.Name]++
		}
	}

	// Queue nodes with zero in-degree.
	queue := make([]string, 0)
	for _, s := range steps {
		if inDegree[s.Name] == 0 {
			queue = append(queue, s.Name)
		}
	}

	sorted := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted++

		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if sorted != len(steps) {
		return fmt.Errorf("cycle detected in step dependencies")
	}
	return nil
}

// resolveParams validates and resolves parameter values against their specifications.
func resolveParams(specs []v1alpha2.ParamSpec, provided map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(specs))

	for _, ps := range specs {
		val, ok := provided[ps.Name]
		if !ok {
			if ps.Default != nil {
				val = *ps.Default
			} else {
				return nil, fmt.Errorf("required parameter %q not provided", ps.Name)
			}
		}

		// Enum validation.
		if len(ps.Enum) > 0 {
			found := false
			for _, e := range ps.Enum {
				if val == e {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("parameter %q value %q not in enum %v", ps.Name, val, ps.Enum)
			}
		}

		// Type validation.
		switch ps.Type {
		case v1alpha2.ParamTypeNumber:
			if _, err := strconv.ParseFloat(val, 64); err != nil {
				return nil, fmt.Errorf("parameter %q: expected number, got %q", ps.Name, val)
			}
		case v1alpha2.ParamTypeBoolean:
			if _, err := strconv.ParseBool(val); err != nil {
				return nil, fmt.Errorf("parameter %q: expected boolean, got %q", ps.Name, val)
			}
		case v1alpha2.ParamTypeString, "":
			// All values are valid strings.
		}

		resolved[ps.Name] = val
	}

	return resolved, nil
}

// mergePolicies merges step-level policies with template defaults.
// Step-level policies take precedence over defaults.
func mergePolicies(stepPolicy *v1alpha2.StepPolicy, defaults *v1alpha2.StepPolicyDefaults) *v1alpha2.StepPolicy {
	if defaults == nil && stepPolicy == nil {
		return nil
	}
	if defaults == nil {
		return stepPolicy
	}

	result := &v1alpha2.StepPolicy{}

	// Merge retry policy.
	if stepPolicy != nil && stepPolicy.Retry != nil {
		result.Retry = stepPolicy.Retry
	} else if defaults.Retry != nil {
		result.Retry = &v1alpha2.WorkflowRetryPolicy{
			MaxAttempts:        defaults.Retry.MaxAttempts,
			InitialInterval:    defaults.Retry.InitialInterval,
			MaximumInterval:    defaults.Retry.MaximumInterval,
			BackoffCoefficient: defaults.Retry.BackoffCoefficient,
			NonRetryableErrors: defaults.Retry.NonRetryableErrors,
		}
	}

	// Merge timeout policy.
	if stepPolicy != nil && stepPolicy.Timeout != nil {
		result.Timeout = stepPolicy.Timeout
	} else if defaults.Timeout != nil {
		result.Timeout = &v1alpha2.WorkflowTimeoutPolicy{
			StartToClose:    defaults.Timeout.StartToClose,
			ScheduleToClose: defaults.Timeout.ScheduleToClose,
			Heartbeat:       defaults.Timeout.Heartbeat,
		}
	}

	if result.Retry == nil && result.Timeout == nil {
		return nil
	}
	return result
}
