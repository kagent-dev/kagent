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
	"fmt"
	"strings"
	"testing"
	"time"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ptr(s string) *string { return &s }

func TestDAGCompiler_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    v1alpha2.WorkflowTemplateSpec
		wantErr string
	}{
		{
			name: "valid linear DAG A->B->C",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a"},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "do-b", DependsOn: []string{"a"}},
					{Name: "c", Type: v1alpha2.StepTypeAction, Action: "do-c", DependsOn: []string{"b"}},
				},
			},
			wantErr: "",
		},
		{
			name: "valid parallel DAG A->[B,C]->D",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a"},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "do-b", DependsOn: []string{"a"}},
					{Name: "c", Type: v1alpha2.StepTypeAction, Action: "do-c", DependsOn: []string{"a"}},
					{Name: "d", Type: v1alpha2.StepTypeAction, Action: "do-d", DependsOn: []string{"b", "c"}},
				},
			},
			wantErr: "",
		},
		{
			name: "valid agent step",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "analyze", Type: v1alpha2.StepTypeAgent, AgentRef: "my-agent", Prompt: "analyze this"},
				},
			},
			wantErr: "",
		},
		{
			name:    "empty steps",
			spec:    v1alpha2.WorkflowTemplateSpec{Steps: []v1alpha2.StepSpec{}},
			wantErr: "at least one step",
		},
		{
			name: "duplicate step names",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a"},
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-b"},
				},
			},
			wantErr: "duplicate step name",
		},
		{
			name: "dependency on nonexistent step",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a", DependsOn: []string{"missing"}},
				},
			},
			wantErr: "nonexistent step",
		},
		{
			name: "self dependency",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a", DependsOn: []string{"a"}},
				},
			},
			wantErr: "depend on itself",
		},
		{
			name: "cycle A->B->C->A",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a", DependsOn: []string{"c"}},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "do-b", DependsOn: []string{"a"}},
					{Name: "c", Type: v1alpha2.StepTypeAction, Action: "do-c", DependsOn: []string{"b"}},
				},
			},
			wantErr: "cycle detected",
		},
		{
			name: "action step missing action field",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction},
				},
			},
			wantErr: "must have 'action' field",
		},
		{
			name: "agent step missing agentRef",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAgent},
				},
			},
			wantErr: "must have 'agentRef' field",
		},
	}

	compiler := NewDAGCompiler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := compiler.Validate(&tt.spec)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestDAGCompiler_Compile(t *testing.T) {
	tests := []struct {
		name       string
		spec       v1alpha2.WorkflowTemplateSpec
		params     map[string]string
		wantErr    string
		wantSteps  int
		checkPlan  func(t *testing.T, plan *ExecutionPlan)
	}{
		{
			name: "simple compile with params",
			spec: v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "url", Type: v1alpha2.ParamTypeString},
					{Name: "retries", Type: v1alpha2.ParamTypeNumber, Default: ptr("3")},
				},
				Steps: []v1alpha2.StepSpec{
					{Name: "fetch", Type: v1alpha2.StepTypeAction, Action: "http.request",
						With: map[string]string{"url": "${{ params.url }}"}},
				},
			},
			params:    map[string]string{"url": "https://example.com"},
			wantSteps: 1,
			checkPlan: func(t *testing.T, plan *ExecutionPlan) {
				if plan.Params["url"] != "https://example.com" {
					t.Errorf("expected param url=https://example.com, got %q", plan.Params["url"])
				}
				if plan.Params["retries"] != "3" {
					t.Errorf("expected param retries=3, got %q", plan.Params["retries"])
				}
			},
		},
		{
			name: "missing required param",
			spec: v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "url", Type: v1alpha2.ParamTypeString},
				},
				Steps: []v1alpha2.StepSpec{
					{Name: "fetch", Type: v1alpha2.StepTypeAction, Action: "http.request"},
				},
			},
			params:  map[string]string{},
			wantErr: "required parameter",
		},
		{
			name: "invalid enum value",
			spec: v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "env", Type: v1alpha2.ParamTypeString, Enum: []string{"dev", "staging", "prod"}},
				},
				Steps: []v1alpha2.StepSpec{
					{Name: "deploy", Type: v1alpha2.StepTypeAction, Action: "deploy"},
				},
			},
			params:  map[string]string{"env": "local"},
			wantErr: "not in enum",
		},
		{
			name: "invalid number param",
			spec: v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "count", Type: v1alpha2.ParamTypeNumber},
				},
				Steps: []v1alpha2.StepSpec{
					{Name: "run", Type: v1alpha2.StepTypeAction, Action: "run"},
				},
			},
			params:  map[string]string{"count": "not-a-number"},
			wantErr: "expected number",
		},
		{
			name: "invalid boolean param",
			spec: v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "verbose", Type: v1alpha2.ParamTypeBoolean},
				},
				Steps: []v1alpha2.StepSpec{
					{Name: "run", Type: v1alpha2.StepTypeAction, Action: "run"},
				},
			},
			params:  map[string]string{"verbose": "maybe"},
			wantErr: "expected boolean",
		},
		{
			name: "policy merging - step overrides defaults",
			spec: v1alpha2.WorkflowTemplateSpec{
				Defaults: &v1alpha2.StepPolicyDefaults{
					Retry: &v1alpha2.WorkflowRetryPolicy{
						MaxAttempts: 5,
					},
					Timeout: &v1alpha2.WorkflowTimeoutPolicy{
						StartToClose: metav1.Duration{Duration: 10 * time.Minute},
					},
				},
				Steps: []v1alpha2.StepSpec{
					{
						Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a",
						Policy: &v1alpha2.StepPolicy{
							Retry: &v1alpha2.WorkflowRetryPolicy{MaxAttempts: 2},
						},
					},
					{Name: "b", Type: v1alpha2.StepTypeAction, Action: "do-b"},
				},
			},
			params:    map[string]string{},
			wantSteps: 2,
			checkPlan: func(t *testing.T, plan *ExecutionPlan) {
				// Step A: step-level retry overrides default, but timeout from defaults
				stepA := plan.Steps[0]
				if stepA.Policy == nil {
					t.Fatal("step a should have merged policy")
				}
				if stepA.Policy.Retry.MaxAttempts != 2 {
					t.Errorf("step a retry: want 2, got %d", stepA.Policy.Retry.MaxAttempts)
				}
				if stepA.Policy.Timeout.StartToClose.Duration != 10*time.Minute {
					t.Errorf("step a timeout: want 10m, got %v", stepA.Policy.Timeout.StartToClose.Duration)
				}

				// Step B: inherits all defaults
				stepB := plan.Steps[1]
				if stepB.Policy == nil {
					t.Fatal("step b should have default policy")
				}
				if stepB.Policy.Retry.MaxAttempts != 5 {
					t.Errorf("step b retry: want 5, got %d", stepB.Policy.Retry.MaxAttempts)
				}
			},
		},
		{
			name: "plan includes workflowID and taskQueue",
			spec: v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "a", Type: v1alpha2.StepTypeAction, Action: "do-a"},
				},
			},
			params:    map[string]string{},
			wantSteps: 1,
			checkPlan: func(t *testing.T, plan *ExecutionPlan) {
				if plan.WorkflowID != "test-wf-id" {
					t.Errorf("expected workflowID=test-wf-id, got %q", plan.WorkflowID)
				}
				if plan.TaskQueue != "kagent-workflows" {
					t.Errorf("expected taskQueue=kagent-workflows, got %q", plan.TaskQueue)
				}
			},
		},
	}

	compiler := NewDAGCompiler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := compiler.Compile(&tt.spec, tt.params, "test-wf-id", "kagent-workflows")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Compile() unexpected error: %v", err)
				}
				if tt.wantSteps > 0 && len(plan.Steps) != tt.wantSteps {
					t.Errorf("expected %d steps, got %d", tt.wantSteps, len(plan.Steps))
				}
				if tt.checkPlan != nil {
					tt.checkPlan(t, plan)
				}
			} else {
				if err == nil {
					t.Errorf("Compile() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Compile() error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestDAGCompiler_Validate_StepCountLimit(t *testing.T) {
	steps := make([]v1alpha2.StepSpec, maxStepCount+1)
	for i := range steps {
		steps[i] = v1alpha2.StepSpec{
			Name:   fmt.Sprintf("step-%d", i),
			Type:   v1alpha2.StepTypeAction,
			Action: "noop",
		}
	}

	compiler := NewDAGCompiler()
	err := compiler.Validate(&v1alpha2.WorkflowTemplateSpec{Steps: steps})
	if err == nil {
		t.Error("expected error for exceeding step count limit")
	}
	if !strings.Contains(err.Error(), "maximum is 200") {
		t.Errorf("expected step count error, got: %v", err)
	}
}

