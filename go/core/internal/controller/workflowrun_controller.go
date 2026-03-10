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

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// workflowTaskQueue is the default task queue for DAG workflows.
	workflowTaskQueue = "kagent-workflows"
)

// TemporalWorkflowClient abstracts Temporal client operations for testability.
type TemporalWorkflowClient interface {
	// StartWorkflow starts a new Temporal workflow and returns the workflow ID.
	StartWorkflow(ctx context.Context, workflowID, taskQueue string, plan *compiler.ExecutionPlan) error
	// CancelWorkflow cancels a running Temporal workflow.
	CancelWorkflow(ctx context.Context, workflowID string) error
}

// WorkflowRunController reconciles WorkflowRun objects.
// It validates params, snapshots the template, submits to Temporal, and handles cleanup.
type WorkflowRunController struct {
	client.Client
	Scheme         *runtime.Scheme
	Compiler       *compiler.DAGCompiler
	TemporalClient TemporalWorkflowClient
}

// +kubebuilder:rbac:groups=kagent.dev,resources=workflowruns,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=workflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=workflowruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=kagent.dev,resources=workflowtemplates,verbs=get;list;watch

func (r *WorkflowRunController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var run v1alpha2.WorkflowRun
	if err := r.Get(ctx, req.NamespacedName, &run); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion.
	if !run.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &run)
	}

	// Phase 1: Accept — resolve template, validate params, snapshot.
	if !isConditionTrue(run.Status.Conditions, v1alpha2.WorkflowRunConditionAccepted) {
		return r.handleAcceptance(ctx, &run)
	}

	// Phase 2: Submit — compile and start Temporal workflow.
	if run.Status.TemporalWorkflowID == "" {
		return r.handleSubmission(ctx, &run)
	}

	return ctrl.Result{}, nil
}

// handleAcceptance resolves the template, validates params, snapshots the spec, and adds finalizer.
func (r *WorkflowRunController) handleAcceptance(ctx context.Context, run *v1alpha2.WorkflowRun) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Resolve template.
	var template v1alpha2.WorkflowTemplate
	templateKey := types.NamespacedName{
		Name:      run.Spec.WorkflowTemplateRef,
		Namespace: run.Namespace,
	}
	if err := r.Get(ctx, templateKey, &template); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return r.setAcceptedFalse(ctx, run, "TemplateNotFound",
				fmt.Sprintf("WorkflowTemplate %q not found", run.Spec.WorkflowTemplateRef))
		}
		return ctrl.Result{}, fmt.Errorf("failed to get WorkflowTemplate: %w", err)
	}

	// Check template is validated.
	if !template.Status.Validated {
		return r.setAcceptedFalse(ctx, run, "TemplateNotValidated",
			fmt.Sprintf("WorkflowTemplate %q has not passed validation", run.Spec.WorkflowTemplateRef))
	}

	// Validate params against template spec.
	paramMap := paramsToMap(run.Spec.Params)
	if _, err := r.Compiler.Compile(&template.Spec, paramMap, "validate", "validate"); err != nil {
		return r.setAcceptedFalse(ctx, run, "InvalidParams", err.Error())
	}

	logger.Info("Accepting WorkflowRun", "name", run.Name, "template", run.Spec.WorkflowTemplateRef)

	// Add finalizer.
	if !controllerutil.ContainsFinalizer(run, v1alpha2.WorkflowRunFinalizer) {
		controllerutil.AddFinalizer(run, v1alpha2.WorkflowRunFinalizer)
		if err := r.Update(ctx, run); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
	}

	// Snapshot template spec.
	run.Status.ResolvedSpec = template.Spec.DeepCopy()
	run.Status.TemplateGeneration = template.Generation
	run.Status.Phase = v1alpha2.WorkflowRunPhasePending
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.WorkflowRunConditionAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		Message:            "Template resolved and parameters validated",
		ObservedGeneration: run.Generation,
	})

	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update WorkflowRun status: %w", err)
	}

	return ctrl.Result{Requeue: true}, nil
}

// handleSubmission compiles the execution plan and starts the Temporal workflow.
func (r *WorkflowRunController) handleSubmission(ctx context.Context, run *v1alpha2.WorkflowRun) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	workflowID := fmt.Sprintf("wf-%s-%s-%s", run.Namespace, run.Spec.WorkflowTemplateRef, run.Name)
	paramMap := paramsToMap(run.Spec.Params)

	plan, err := r.Compiler.Compile(run.Status.ResolvedSpec, paramMap, workflowID, workflowTaskQueue)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to compile execution plan: %w", err)
	}

	logger.Info("Starting Temporal workflow", "workflowID", workflowID, "steps", len(plan.Steps))

	if err := r.TemporalClient.StartWorkflow(ctx, workflowID, workflowTaskQueue, plan); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to start Temporal workflow: %w", err)
	}

	now := metav1.Now()
	run.Status.TemporalWorkflowID = workflowID
	run.Status.Phase = v1alpha2.WorkflowRunPhaseRunning
	run.Status.StartTime = &now
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.WorkflowRunConditionRunning,
		Status:             metav1.ConditionTrue,
		Reason:             "WorkflowStarted",
		Message:            fmt.Sprintf("Temporal workflow %s started", workflowID),
		ObservedGeneration: run.Generation,
	})

	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update WorkflowRun status: %w", err)
	}

	return ctrl.Result{}, nil
}

// handleDeletion cancels the Temporal workflow and removes the finalizer.
func (r *WorkflowRunController) handleDeletion(ctx context.Context, run *v1alpha2.WorkflowRun) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(run, v1alpha2.WorkflowRunFinalizer) {
		return ctrl.Result{}, nil
	}

	// Cancel Temporal workflow if one was started.
	if run.Status.TemporalWorkflowID != "" && r.TemporalClient != nil {
		logger.Info("Cancelling Temporal workflow", "workflowID", run.Status.TemporalWorkflowID)
		if err := r.TemporalClient.CancelWorkflow(ctx, run.Status.TemporalWorkflowID); err != nil {
			logger.Error(err, "failed to cancel Temporal workflow, proceeding with cleanup",
				"workflowID", run.Status.TemporalWorkflowID)
		}
	}

	controllerutil.RemoveFinalizer(run, v1alpha2.WorkflowRunFinalizer)
	if err := r.Update(ctx, run); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// setAcceptedFalse sets the Accepted condition to False and updates status.
func (r *WorkflowRunController) setAcceptedFalse(ctx context.Context, run *v1alpha2.WorkflowRun, reason, message string) (ctrl.Result, error) {
	run.Status.Phase = v1alpha2.WorkflowRunPhaseFailed
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.WorkflowRunConditionAccepted,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: run.Generation,
	})

	if err := r.Status().Update(ctx, run); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update WorkflowRun status: %w", err)
	}
	return ctrl.Result{}, nil
}

// isConditionTrue checks if a condition with the given type is True.
func isConditionTrue(conditions []metav1.Condition, condType string) bool {
	for _, c := range conditions {
		if c.Type == condType && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// paramsToMap converts a slice of Params to a map.
func paramsToMap(params []v1alpha2.Param) map[string]string {
	m := make(map[string]string, len(params))
	for _, p := range params {
		m[p.Name] = p.Value
	}
	return m
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkflowRunController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha2.WorkflowRun{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("workflowrun").
		Complete(r)
}
