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
	"testing"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestExtractExpressions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
		exprs []Expression
	}{
		{
			name:  "no expressions",
			input: "plain text",
			want:  0,
		},
		{
			name:  "single param",
			input: "${{ params.url }}",
			want:  1,
			exprs: []Expression{{Raw: "${{ params.url }}", Namespace: "params", Path: "url"}},
		},
		{
			name:  "context with dotted path",
			input: "${{ context.checkout.path }}",
			want:  1,
			exprs: []Expression{{Raw: "${{ context.checkout.path }}", Namespace: "context", Path: "checkout.path"}},
		},
		{
			name:  "multiple expressions",
			input: "${{ params.a }}-${{ params.b }}",
			want:  2,
		},
		{
			name:  "escaped expression not extracted",
			input: "$${{ not.resolved }}",
			want:  0,
		},
		{
			name:  "workflow namespace",
			input: "${{ workflow.name }}",
			want:  1,
			exprs: []Expression{{Raw: "${{ workflow.name }}", Namespace: "workflow", Path: "name"}},
		},
		{
			name:  "no closing braces",
			input: "${{ params.url",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractExpressions(tt.input)
			if len(got) != tt.want {
				t.Errorf("ExtractExpressions() returned %d expressions, want %d", len(got), tt.want)
			}
			if tt.exprs != nil {
				for i, e := range tt.exprs {
					if i >= len(got) {
						break
					}
					if got[i].Raw != e.Raw || got[i].Namespace != e.Namespace || got[i].Path != e.Path {
						t.Errorf("expression[%d] = %+v, want %+v", i, got[i], e)
					}
				}
			}
		})
	}
}

func TestResolveExpression(t *testing.T) {
	params := map[string]string{
		"url":    "https://github.com/example/repo",
		"branch": "main",
	}

	ctx := &WorkflowContext{
		StepOutputs: map[string]json.RawMessage{
			"checkout": json.RawMessage(`{"path":"/src","commitSha":"abc123"}`),
			"build":    json.RawMessage(`{"artifact":"/out/build.tar.gz","nested":{"key":"deep"}}`),
		},
		Globals: map[string]string{
			"repoPath": "/src",
		},
		WorkflowName:      "my-workflow",
		WorkflowNamespace: "default",
		WorkflowRunName:   "my-workflow-run-1",
	}

	tests := []struct {
		name    string
		expr    string
		params  map[string]string
		ctx     *WorkflowContext
		want    string
		wantErr bool
	}{
		{
			name:   "no expressions passthrough",
			expr:   "plain text",
			params: params,
			ctx:    ctx,
			want:   "plain text",
		},
		{
			name:   "simple param substitution",
			expr:   "${{ params.url }}",
			params: params,
			ctx:    ctx,
			want:   "https://github.com/example/repo",
		},
		{
			name:   "param in surrounding text",
			expr:   "git clone ${{ params.url }} --branch ${{ params.branch }}",
			params: params,
			ctx:    ctx,
			want:   "git clone https://github.com/example/repo --branch main",
		},
		{
			name:   "context step field",
			expr:   "${{ context.checkout.path }}",
			params: params,
			ctx:    ctx,
			want:   "/src",
		},
		{
			name:   "context step nested field",
			expr:   "${{ context.build.nested.key }}",
			params: params,
			ctx:    ctx,
			want:   "deep",
		},
		{
			name:   "context global key",
			expr:   "${{ context.repoPath }}",
			params: params,
			ctx:    ctx,
			want:   "/src",
		},
		{
			name:   "workflow metadata name",
			expr:   "${{ workflow.name }}",
			params: params,
			ctx:    ctx,
			want:   "my-workflow",
		},
		{
			name:   "workflow metadata namespace",
			expr:   "${{ workflow.namespace }}",
			params: params,
			ctx:    ctx,
			want:   "default",
		},
		{
			name:   "workflow metadata runName",
			expr:   "${{ workflow.runName }}",
			params: params,
			ctx:    ctx,
			want:   "my-workflow-run-1",
		},
		{
			name:   "escape produces literal",
			expr:   "$${{ not.resolved }}",
			params: params,
			ctx:    ctx,
			want:   "${{ not.resolved }}",
		},
		{
			name:   "mixed escape and real expression",
			expr:   "$${{ literal }} and ${{ params.url }}",
			params: params,
			ctx:    ctx,
			want:   "${{ literal }} and https://github.com/example/repo",
		},
		{
			name:    "unknown parameter",
			expr:    "${{ params.missing }}",
			params:  params,
			ctx:     ctx,
			wantErr: true,
		},
		{
			name:    "unknown context step",
			expr:    "${{ context.nonexistent.field }}",
			params:  params,
			ctx:     ctx,
			wantErr: true,
		},
		{
			name:    "unknown context key",
			expr:    "${{ context.unknownKey }}",
			params:  params,
			ctx:     ctx,
			wantErr: true,
		},
		{
			name:    "unknown workflow field",
			expr:    "${{ workflow.unknownField }}",
			params:  params,
			ctx:     ctx,
			wantErr: true,
		},
		{
			name:    "unknown namespace",
			expr:    "${{ foobar.something }}",
			params:  params,
			ctx:     ctx,
			wantErr: true,
		},
		{
			name:    "context not available at compile time",
			expr:    "${{ context.checkout.path }}",
			params:  params,
			ctx:     nil,
			wantErr: true,
		},
		{
			name:   "params resolve without context",
			expr:   "${{ params.url }}",
			params: params,
			ctx:    nil,
			want:   "https://github.com/example/repo",
		},
		{
			name:    "empty param name",
			expr:    "${{ params. }}",
			params:  params,
			ctx:     ctx,
			wantErr: true,
		},
		{
			name:   "context step full output",
			expr:   "${{ context.checkout }}",
			params: params,
			ctx:    ctx,
			want:   `{"path":"/src","commitSha":"abc123"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveExpression(tt.expr, tt.params, tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ResolveExpression() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateExpressions(t *testing.T) {
	tests := []struct {
		name     string
		spec     *v1alpha2.WorkflowTemplateSpec
		wantErrs int
	}{
		{
			name: "valid param references",
			spec: &v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "url"},
					{Name: "branch"},
				},
				Steps: []v1alpha2.StepSpec{
					{
						Name:   "checkout",
						Type:   v1alpha2.StepTypeAction,
						Action: "git-clone",
						Prompt: "Clone ${{ params.url }} on ${{ params.branch }}",
					},
				},
			},
			wantErrs: 0,
		},
		{
			name: "undeclared param reference",
			spec: &v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "url"},
				},
				Steps: []v1alpha2.StepSpec{
					{
						Name:   "checkout",
						Type:   v1alpha2.StepTypeAction,
						Action: "git-clone",
						Prompt: "Clone ${{ params.url }} on ${{ params.branch }}",
					},
				},
			},
			wantErrs: 1,
		},
		{
			name: "context refs not validated statically",
			spec: &v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{
						Name:   "deploy",
						Type:   v1alpha2.StepTypeAction,
						Action: "deploy",
						Prompt: "Deploy ${{ context.build.artifact }}",
					},
				},
			},
			wantErrs: 0,
		},
		{
			name: "undeclared param in with map",
			spec: &v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{
					{Name: "url"},
				},
				Steps: []v1alpha2.StepSpec{
					{
						Name:   "checkout",
						Type:   v1alpha2.StepTypeAction,
						Action: "git-clone",
						With: map[string]string{
							"repo": "${{ params.url }}",
							"ref":  "${{ params.missing }}",
						},
					},
				},
			},
			wantErrs: 1,
		},
		{
			name: "multiple errors across steps",
			spec: &v1alpha2.WorkflowTemplateSpec{
				Params: []v1alpha2.ParamSpec{},
				Steps: []v1alpha2.StepSpec{
					{
						Name:   "step1",
						Type:   v1alpha2.StepTypeAction,
						Action: "do",
						Prompt: "${{ params.a }}",
					},
					{
						Name:   "step2",
						Type:   v1alpha2.StepTypeAction,
						Action: "do",
						Prompt: "${{ params.b }}",
					},
				},
			},
			wantErrs: 2,
		},
		{
			name: "no expressions no errors",
			spec: &v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{
						Name:   "step1",
						Type:   v1alpha2.StepTypeAction,
						Action: "do",
						Prompt: "plain text",
					},
				},
			},
			wantErrs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateExpressions(tt.spec)
			if len(errs) != tt.wantErrs {
				t.Errorf("ValidateExpressions() returned %d errors, want %d: %v", len(errs), tt.wantErrs, errs)
			}
		})
	}
}
