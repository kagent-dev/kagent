/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
)

const (
	// sandboxFinalizer guarantees the backend sandbox is deleted before the
	// Kubernetes object is removed.
	sandboxFinalizer = "kagent.dev/sandbox-backend-cleanup"

	// sandboxNotReadyRequeue is how long we wait before re-polling backend
	// status while the sandbox is still provisioning.
	sandboxNotReadyRequeue = 10 * time.Second
)

// SandboxController reconciles a kagent.dev/v1alpha2 Sandbox against an
// AsyncBackend. It is intentionally independent of the SandboxAgent path —
// Sandboxes are a generic exec/SSH-able environment with no in-cluster
// workload owned by kagent.
type SandboxController struct {
	Client  client.Client
	Backend sandboxbackend.AsyncBackend
}

// +kubebuilder:rbac:groups=kagent.dev,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=sandboxes/finalizers,verbs=update

func (r *SandboxController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("sandbox", req.NamespacedName)

	var sbx v1alpha2.Sandbox
	if err := r.Client.Get(ctx, req.NamespacedName, &sbx); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get sandbox: %w", err)
	}

	if !sbx.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &sbx)
	}

	if controllerutil.AddFinalizer(&sbx, sandboxFinalizer) {
		if err := r.Client.Update(ctx, &sbx); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		// requeue so we see the updated object with a fresh resourceVersion
		return ctrl.Result{Requeue: true}, nil
	}

	if r.Backend == nil || r.Backend.Name() != sbx.Spec.Backend {
		setCondition(&sbx, v1alpha2.SandboxConditionTypeAccepted, metav1.ConditionFalse,
			"BackendUnavailable",
			fmt.Sprintf("no backend configured for %q", sbx.Spec.Backend))
		setCondition(&sbx, v1alpha2.SandboxConditionTypeReady, metav1.ConditionFalse,
			"BackendUnavailable", "")
		if err := r.patchStatus(ctx, &sbx); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	res, err := r.Backend.EnsureSandbox(ctx, &sbx)
	if err != nil {
		log.Error(err, "EnsureSandbox failed")
		setCondition(&sbx, v1alpha2.SandboxConditionTypeAccepted, metav1.ConditionFalse,
			"EnsureFailed", err.Error())
		setCondition(&sbx, v1alpha2.SandboxConditionTypeReady, metav1.ConditionFalse,
			"EnsureFailed", err.Error())
		if perr := r.patchStatus(ctx, &sbx); perr != nil {
			return ctrl.Result{}, perr
		}
		return ctrl.Result{}, err
	}

	sbx.Status.BackendRef = &v1alpha2.SandboxStatusRef{
		Backend: r.Backend.Name(),
		ID:      res.Handle.ID,
	}
	if res.Endpoint != "" {
		sbx.Status.Connection = &v1alpha2.SandboxConnection{Endpoint: res.Endpoint}
	}
	setCondition(&sbx, v1alpha2.SandboxConditionTypeAccepted, metav1.ConditionTrue,
		"SandboxAccepted", "backend accepted sandbox request")

	st, reason, msg := r.Backend.GetStatus(ctx, res.Handle)
	setCondition(&sbx, v1alpha2.SandboxConditionTypeReady, st, reason, msg)
	sbx.Status.ObservedGeneration = sbx.Generation

	if err := r.patchStatus(ctx, &sbx); err != nil {
		return ctrl.Result{}, err
	}

	if st != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: sandboxNotReadyRequeue}, nil
	}
	return ctrl.Result{}, nil
}

func (r *SandboxController) reconcileDelete(ctx context.Context, sbx *v1alpha2.Sandbox) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sbx, sandboxFinalizer) {
		return ctrl.Result{}, nil
	}

	if r.Backend != nil && sbx.Status.BackendRef != nil && sbx.Status.BackendRef.ID != "" {
		if err := r.Backend.DeleteSandbox(ctx, sandboxbackend.Handle{ID: sbx.Status.BackendRef.ID}); err != nil {
			return ctrl.Result{RequeueAfter: sandboxNotReadyRequeue}, err
		}
	}

	controllerutil.RemoveFinalizer(sbx, sandboxFinalizer)
	if err := r.Client.Update(ctx, sbx); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *SandboxController) patchStatus(ctx context.Context, sbx *v1alpha2.Sandbox) error {
	if err := r.Client.Status().Update(ctx, sbx); err != nil {
		return fmt.Errorf("update sandbox status: %w", err)
	}
	return nil
}

// setCondition upserts a condition on sbx.Status.Conditions.
func setCondition(sbx *v1alpha2.Sandbox, t string, s metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i := range sbx.Status.Conditions {
		c := &sbx.Status.Conditions[i]
		if c.Type != t {
			continue
		}
		if c.Status != s {
			c.LastTransitionTime = now
		}
		c.Status = s
		c.Reason = reason
		c.Message = msg
		c.ObservedGeneration = sbx.Generation
		return
	}
	sbx.Status.Conditions = append(sbx.Status.Conditions, metav1.Condition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
		ObservedGeneration: sbx.Generation,
	})
}

// SetupWithManager registers the controller with the manager.
func (r *SandboxController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&v1alpha2.Sandbox{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		))).
		Named("sandbox").
		Complete(r)
}
