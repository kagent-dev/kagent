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
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
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
	// agentHarnessFinalizer guarantees the backend sandbox is deleted before the
	// Kubernetes object is removed.
	agentHarnessFinalizer = "kagent.dev/agent-harness-backend-cleanup"

	// agentHarnessNotReadyRequeue is how long we wait before re-polling backend
	// status while the sandbox is still provisioning.
	agentHarnessNotReadyRequeue = 10 * time.Second

	// annotationAgentHarnessBootstrapGeneration records the AgentHarness metadata.generation for which
	// post-ready bootstrap (backend OnAgentHarnessReady, e.g. exec hooks) already completed.
	annotationAgentHarnessBootstrapGeneration = "kagent.dev/agent-harness-bootstrap-generation"
)

// AgentHarnessController reconciles a kagent.dev/v1alpha2 AgentHarness against an
// AsyncBackend. It is intentionally independent of the SandboxAgent path —
// harness VMs are a generic exec/SSH-able environment with no in-cluster
// workload owned by kagent.
type AgentHarnessController struct {
	Client   client.Client
	Recorder events.EventRecorder
	Backends map[v1alpha2.AgentHarnessBackendType]sandboxbackend.AsyncBackend
}

// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses/finalizers,verbs=update

func (r *AgentHarnessController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("agentHarness", req.NamespacedName)

	var ah v1alpha2.AgentHarness
	if err := r.Client.Get(ctx, req.NamespacedName, &ah); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get AgentHarness: %w", err)
	}

	if !ah.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &ah)
	}

	if controllerutil.AddFinalizer(&ah, agentHarnessFinalizer) {
		if err := r.Client.Update(ctx, &ah); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	backend := r.Backends[ah.Spec.Backend]
	if backend == nil {
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
			"BackendUnavailable",
			fmt.Sprintf("no backend configured for %q", ah.Spec.Backend))
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"BackendUnavailable", "")
		if err := r.patchAgentHarnessStatus(ctx, &ah); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	res, err := backend.EnsureAgentHarness(ctx, &ah)
	if err != nil {
		log.Error(err, "EnsureAgentHarness failed")
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
			"EnsureFailed", err.Error())
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"EnsureFailed", err.Error())
		if perr := r.patchAgentHarnessStatus(ctx, &ah); perr != nil {
			return ctrl.Result{}, perr
		}
		return ctrl.Result{}, err
	}

	ah.Status.BackendRef = &v1alpha2.AgentHarnessStatusRef{
		Backend: ah.Spec.Backend,
		ID:      res.Handle.ID,
	}
	if res.Endpoint != "" {
		ah.Status.Connection = &v1alpha2.AgentHarnessConnection{Endpoint: res.Endpoint}
	}
	setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionTrue,
		"AgentHarnessAccepted", "backend accepted sandbox request")

	st, reason, msg := backend.GetStatus(ctx, res.Handle)
	pending := r.postReadyBootstrapPending(&ah)
	if st == metav1.ConditionTrue && pending {
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"BootstrapPending",
			"gateway sandbox is ready; waiting for post-ready bootstrap (OnAgentHarnessReady) to finish")
	} else {
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, st, reason, msg)
	}
	ah.Status.ObservedGeneration = ah.Generation

	if err := r.patchAgentHarnessStatus(ctx, &ah); err != nil {
		return ctrl.Result{}, err
	}

	if st != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
	}
	if pending {
		if err := r.maybePostReadyBootstrap(ctx, client.ObjectKeyFromObject(&ah), &ah, res.Handle, backend); err != nil {
			log.Error(err, "post-ready sandbox bootstrap failed")
			return ctrl.Result{}, err
		}
		var latest v1alpha2.AgentHarness
		if err := r.Client.Get(ctx, req.NamespacedName, &latest); err != nil {
			return ctrl.Result{}, fmt.Errorf("get AgentHarness after bootstrap: %w", err)
		}
		st2, reason2, msg2 := backend.GetStatus(ctx, res.Handle)
		setAgentHarnessCondition(&latest, v1alpha2.AgentHarnessConditionTypeReady, st2, reason2, msg2)
		latest.Status.ObservedGeneration = latest.Generation
		if err := r.Client.Status().Update(ctx, &latest); err != nil {
			return ctrl.Result{}, fmt.Errorf("update AgentHarness status after bootstrap: %w", err)
		}
	}
	return ctrl.Result{}, nil
}

func (r *AgentHarnessController) postReadyBootstrapPending(ah *v1alpha2.AgentHarness) bool {
	wantGen := strconv.FormatInt(ah.Generation, 10)
	if ah.Annotations != nil && ah.Annotations[annotationAgentHarnessBootstrapGeneration] == wantGen {
		return false
	}
	return true
}

func (r *AgentHarnessController) maybePostReadyBootstrap(ctx context.Context, key client.ObjectKey, ah *v1alpha2.AgentHarness, h sandboxbackend.Handle, async sandboxbackend.AsyncBackend) error {
	if !r.postReadyBootstrapPending(ah) {
		return nil
	}
	wantGen := strconv.FormatInt(ah.Generation, 10)
	if err := async.OnAgentHarnessReady(ctx, ah, h); err != nil {
		return err
	}
	var fresh v1alpha2.AgentHarness
	if err := r.Client.Get(ctx, key, &fresh); err != nil {
		return fmt.Errorf("get AgentHarness after bootstrap: %w", err)
	}
	base := fresh.DeepCopy()
	if fresh.Annotations == nil {
		fresh.Annotations = map[string]string{}
	}
	fresh.Annotations[annotationAgentHarnessBootstrapGeneration] = wantGen
	if err := r.Client.Patch(ctx, &fresh, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch AgentHarness bootstrap-generation annotation: %w", err)
	}
	ctrl.LoggerFrom(ctx).WithValues("agentHarness", key.String()).Info(
		"recorded post-ready bootstrap for AgentHarness generation", "generation", ah.Generation)
	return nil
}

func (r *AgentHarnessController) reconcileDelete(ctx context.Context, ah *v1alpha2.AgentHarness) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ah, agentHarnessFinalizer) {
		return ctrl.Result{}, nil
	}

	if ah.Status.BackendRef != nil && ah.Status.BackendRef.ID != "" {
		del := r.Backends[ah.Status.BackendRef.Backend]
		if del != nil {
			if err := del.DeleteAgentHarness(ctx, sandboxbackend.Handle{ID: ah.Status.BackendRef.ID}); err != nil {
				if r.Recorder != nil {
					r.Recorder.Eventf(ah, nil, "Warning", "AgentHarnessDeleteFailed", "DeleteAgentHarness", "%s", err.Error())
				}
				return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
			}
		}
	}

	controllerutil.RemoveFinalizer(ah, agentHarnessFinalizer)
	if err := r.Client.Update(ctx, ah); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *AgentHarnessController) patchAgentHarnessStatus(ctx context.Context, ah *v1alpha2.AgentHarness) error {
	if err := r.Client.Status().Update(ctx, ah); err != nil {
		return fmt.Errorf("update AgentHarness status: %w", err)
	}
	return nil
}

func setAgentHarnessCondition(ah *v1alpha2.AgentHarness, t string, s metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i := range ah.Status.Conditions {
		c := &ah.Status.Conditions[i]
		if c.Type != t {
			continue
		}
		if c.Status != s {
			c.LastTransitionTime = now
		}
		c.Status = s
		c.Reason = reason
		c.Message = msg
		c.ObservedGeneration = ah.Generation
		return
	}
	ah.Status.Conditions = append(ah.Status.Conditions, metav1.Condition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
		ObservedGeneration: ah.Generation,
	})
}

// SetupWithManager registers the controller with the manager.
func (r *AgentHarnessController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&v1alpha2.AgentHarness{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		))).
		Named("agentharness").
		Complete(r)
}
