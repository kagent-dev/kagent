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

package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// WorkflowTemplateConditionAccepted indicates whether the template passed validation.
	WorkflowTemplateConditionAccepted = "Accepted"
)

// WorkflowTemplateController reconciles WorkflowTemplate objects.
// It validates the DAG structure on create/update and updates status conditions.
type WorkflowTemplateController struct {
	client.Client
	Scheme   *runtime.Scheme
	Compiler *compiler.DAGCompiler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=workflowtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=kagent.dev,resources=workflowtemplates/status,verbs=get;update;patch

func (r *WorkflowTemplateController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var template v1alpha2.WorkflowTemplate
	if err := r.Get(ctx, req.NamespacedName, &template); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if already reconciled for this generation.
	if template.Status.ObservedGeneration == template.Generation {
		return ctrl.Result{}, nil
	}

	logger.Info("Validating WorkflowTemplate", "name", template.Name)

	if err := r.Compiler.Validate(&template.Spec); err != nil {
		reason := classifyValidationError(err)
		meta.SetStatusCondition(&template.Status.Conditions, metav1.Condition{
			Type:               WorkflowTemplateConditionAccepted,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            err.Error(),
			ObservedGeneration: template.Generation,
		})
		template.Status.Validated = false
		template.Status.StepCount = int32(len(template.Spec.Steps))
	} else {
		meta.SetStatusCondition(&template.Status.Conditions, metav1.Condition{
			Type:               WorkflowTemplateConditionAccepted,
			Status:             metav1.ConditionTrue,
			Reason:             "Valid",
			Message:            "Template DAG is valid",
			ObservedGeneration: template.Generation,
		})
		template.Status.Validated = true
		template.Status.StepCount = int32(len(template.Spec.Steps))
	}

	template.Status.ObservedGeneration = template.Generation
	if err := r.Status().Update(ctx, &template); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update WorkflowTemplate status: %w", err)
	}

	return ctrl.Result{}, nil
}

// classifyValidationError maps compiler error messages to condition reasons.
func classifyValidationError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "cycle detected"):
		return "CycleDetected"
	case strings.Contains(msg, "duplicate step name"):
		return "DuplicateStepName"
	case strings.Contains(msg, "nonexistent step"),
		strings.Contains(msg, "depends on itself"):
		return "InvalidReference"
	case strings.Contains(msg, "exceeds maximum"):
		return "TooManySteps"
	case strings.Contains(msg, "must have"):
		return "InvalidStepSpec"
	default:
		return "ValidationFailed"
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkflowTemplateController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha2.WorkflowTemplate{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("workflowtemplate").
		Complete(r)
}
