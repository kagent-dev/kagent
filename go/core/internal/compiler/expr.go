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
	"strings"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// Expression represents a parsed ${{ }} token found in a string.
type Expression struct {
	// Raw is the full match including delimiters, e.g. "${{ params.url }}".
	Raw string
	// Namespace is the first segment: "params", "context", or "workflow".
	Namespace string
	// Path is everything after the namespace dot, e.g. "url" or "checkout.path".
	Path string
}

// WorkflowContext holds step outputs and workflow metadata used during expression resolution.
type WorkflowContext struct {
	// StepOutputs maps step name (or alias) to JSON-encoded output.
	StepOutputs map[string]json.RawMessage
	// Globals maps top-level context keys (from output.keys) to their values.
	Globals map[string]string
	// WorkflowName is the workflow template name.
	WorkflowName string
	// WorkflowNamespace is the Kubernetes namespace.
	WorkflowNamespace string
	// WorkflowRunName is the WorkflowRun resource name.
	WorkflowRunName string
}

// ExtractExpressions parses all ${{ }} tokens from a string.
// Escaped expressions ($${{ }}) are not included.
func ExtractExpressions(s string) []Expression {
	var exprs []Expression
	remaining := s
	for {
		idx := strings.Index(remaining, "${{")
		if idx < 0 {
			break
		}
		// Check for escape: $$ prefix.
		if idx > 0 && remaining[idx-1] == '$' {
			remaining = remaining[idx+3:]
			continue
		}
		end := strings.Index(remaining[idx:], "}}")
		if end < 0 {
			break
		}
		end += idx // Absolute position of "}}".
		raw := remaining[idx : end+2]
		inner := strings.TrimSpace(remaining[idx+3 : end])

		parts := strings.SplitN(inner, ".", 2)
		expr := Expression{Raw: raw, Namespace: parts[0]}
		if len(parts) > 1 {
			expr.Path = parts[1]
		}
		exprs = append(exprs, expr)
		remaining = remaining[end+2:]
	}
	return exprs
}

// ResolveExpression resolves all ${{ }} expressions in a string.
// params provides parameter values, ctx provides step outputs and metadata.
// ctx may be nil if only param resolution is needed (compile-time).
func ResolveExpression(expr string, params map[string]string, ctx *WorkflowContext) (string, error) {
	result := expr
	// Process escapes first: replace $${{ with a placeholder.
	const escapePlaceholder = "\x00EXPR_ESCAPE\x00"
	result = strings.ReplaceAll(result, "$${{", escapePlaceholder)

	tokens := ExtractExpressions(result)
	for _, tok := range tokens {
		resolved, err := resolveToken(tok, params, ctx)
		if err != nil {
			return "", err
		}
		result = strings.Replace(result, tok.Raw, resolved, 1)
	}

	// Restore escaped expressions.
	result = strings.ReplaceAll(result, escapePlaceholder, "${{")
	return result, nil
}

// resolveToken resolves a single expression token.
func resolveToken(tok Expression, params map[string]string, ctx *WorkflowContext) (string, error) {
	switch tok.Namespace {
	case "params":
		if tok.Path == "" {
			return "", fmt.Errorf("expression %q: missing parameter name", tok.Raw)
		}
		val, ok := params[tok.Path]
		if !ok {
			return "", fmt.Errorf("expression %q: unknown parameter %q", tok.Raw, tok.Path)
		}
		return val, nil

	case "context":
		if ctx == nil {
			return "", fmt.Errorf("expression %q: context not available at compile time", tok.Raw)
		}
		if tok.Path == "" {
			return "", fmt.Errorf("expression %q: missing context path", tok.Raw)
		}
		return resolveContextPath(tok, ctx)

	case "workflow":
		if ctx == nil {
			return "", fmt.Errorf("expression %q: workflow metadata not available at compile time", tok.Raw)
		}
		return resolveWorkflowMeta(tok, ctx)

	default:
		return "", fmt.Errorf("expression %q: unknown namespace %q (expected params, context, or workflow)", tok.Raw, tok.Namespace)
	}
}

// resolveContextPath resolves a context.stepName.field or context.globalKey expression.
func resolveContextPath(tok Expression, ctx *WorkflowContext) (string, error) {
	parts := strings.SplitN(tok.Path, ".", 2)
	stepOrKey := parts[0]

	// Try step output first (context.stepName.field).
	if len(parts) == 2 {
		raw, ok := ctx.StepOutputs[stepOrKey]
		if !ok {
			return "", fmt.Errorf("expression %q: no output from step %q", tok.Raw, stepOrKey)
		}
		return extractJSONField(tok.Raw, raw, parts[1])
	}

	// Single segment: try step output (returns full JSON), then globals.
	if raw, ok := ctx.StepOutputs[stepOrKey]; ok {
		// Return the raw JSON as a string.
		return strings.TrimSpace(string(raw)), nil
	}
	if val, ok := ctx.Globals[stepOrKey]; ok {
		return val, nil
	}
	return "", fmt.Errorf("expression %q: unknown context key %q", tok.Raw, stepOrKey)
}

// extractJSONField extracts a field from JSON data. Supports dotted paths for nested access.
func extractJSONField(rawExpr string, data json.RawMessage, field string) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", fmt.Errorf("expression %q: step output is not a JSON object: %w", rawExpr, err)
	}

	parts := strings.SplitN(field, ".", 2)
	val, ok := obj[parts[0]]
	if !ok {
		return "", fmt.Errorf("expression %q: field %q not found in step output", rawExpr, parts[0])
	}

	// Nested access.
	if len(parts) == 2 {
		return extractJSONField(rawExpr, val, parts[1])
	}

	// Unwrap JSON strings, return other types as-is.
	var s string
	if err := json.Unmarshal(val, &s); err == nil {
		return s, nil
	}
	return strings.TrimSpace(string(val)), nil
}

// resolveWorkflowMeta resolves workflow.* expressions.
func resolveWorkflowMeta(tok Expression, ctx *WorkflowContext) (string, error) {
	switch tok.Path {
	case "name":
		return ctx.WorkflowName, nil
	case "namespace":
		return ctx.WorkflowNamespace, nil
	case "runName":
		return ctx.WorkflowRunName, nil
	default:
		return "", fmt.Errorf("expression %q: unknown workflow field %q (expected name, namespace, or runName)", tok.Raw, tok.Path)
	}
}

// ValidateExpressions statically checks all ${{ params.* }} references in a WorkflowTemplateSpec
// to ensure they refer to declared parameters. Context references are not validated here
// since they depend on runtime execution order.
func ValidateExpressions(spec *v1alpha2.WorkflowTemplateSpec) []error {
	paramNames := make(map[string]bool, len(spec.Params))
	for _, p := range spec.Params {
		paramNames[p.Name] = true
	}

	var errs []error
	for _, step := range spec.Steps {
		// Check prompt field.
		if step.Prompt != "" {
			errs = append(errs, validateParamRefs(step.Name, "prompt", step.Prompt, paramNames)...)
		}
		// Check action field.
		if step.Action != "" {
			errs = append(errs, validateParamRefs(step.Name, "action", step.Action, paramNames)...)
		}
		// Check with map values.
		for k, v := range step.With {
			errs = append(errs, validateParamRefs(step.Name, fmt.Sprintf("with[%s]", k), v, paramNames)...)
		}
	}
	return errs
}

// validateParamRefs checks that all ${{ params.* }} expressions in a string refer to declared params.
func validateParamRefs(stepName, fieldName, value string, paramNames map[string]bool) []error {
	var errs []error
	for _, expr := range ExtractExpressions(value) {
		if expr.Namespace == "params" {
			if expr.Path == "" {
				errs = append(errs, fmt.Errorf("step %q field %q: expression %q missing parameter name", stepName, fieldName, expr.Raw))
			} else if !paramNames[expr.Path] {
				errs = append(errs, fmt.Errorf("step %q field %q: expression %q references undeclared parameter %q", stepName, fieldName, expr.Raw, expr.Path))
			}
		}
	}
	return errs
}
