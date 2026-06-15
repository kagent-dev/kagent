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
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
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
)

// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses/finalizers,verbs=update

func reconcileBackendUnavailable(ctx context.Context, kube client.Client, ah *v1alpha2.AgentHarness, runtime v1alpha2.AgentHarnessRuntime) (ctrl.Result, error) {
	setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
		"BackendUnavailable",
		fmt.Sprintf("no %s backend configured for %q", runtime, ah.Spec.Backend))
	setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
		"BackendUnavailable", "")
	if err := patchAgentHarnessStatus(ctx, kube, ah); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func postReadyBootstrapPending(ah *v1alpha2.AgentHarness) bool {
	cond := meta.FindStatusCondition(ah.Status.Conditions, v1alpha2.AgentHarnessConditionTypeBootstrapReady)
	return cond == nil || cond.ObservedGeneration != ah.Generation || cond.Status != metav1.ConditionTrue
}

func maybePostReadyBootstrap(ctx context.Context, key client.ObjectKey, ah *v1alpha2.AgentHarness, h sandboxbackend.Handle, async sandboxbackend.AsyncBackend) error {
	if !postReadyBootstrapPending(ah) {
		return nil
	}
	if err := async.OnAgentHarnessReady(ctx, ah, h); err != nil {
		return err
	}
	ctrl.LoggerFrom(ctx).WithValues("agentHarness", key.String()).Info(
		"recorded post-ready bootstrap for AgentHarness generation", "generation", ah.Generation)
	return nil
}

func patchAgentHarnessStatus(ctx context.Context, kube client.Client, ah *v1alpha2.AgentHarness) error {
	var current v1alpha2.AgentHarness
	if err := kube.Get(ctx, client.ObjectKeyFromObject(ah), &current); err != nil {
		return fmt.Errorf("get AgentHarness before status update: %w", err)
	}
	if reflect.DeepEqual(current.Status, ah.Status) {
		*ah = current
		return nil
	}
	current.Status = ah.Status
	if err := kube.Status().Update(ctx, &current); err != nil {
		return fmt.Errorf("update AgentHarness status: %w", err)
	}
	*ah = current
	return nil
}

func effectiveAgentHarnessRuntime(ah *v1alpha2.AgentHarness) v1alpha2.AgentHarnessRuntime {
	if ah.Spec.Runtime == "" {
		return v1alpha2.AgentHarnessRuntimeOpenshell
	}
	return ah.Spec.Runtime
}

func setAgentHarnessCondition(ah *v1alpha2.AgentHarness, t string, s metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&ah.Status.Conditions, metav1.Condition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: ah.Generation,
	})
}

func agentHarnessPrimaryPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return true
			}
			if e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() {
				return true
			}
			if !reflect.DeepEqual(e.ObjectNew.GetLabels(), e.ObjectOld.GetLabels()) {
				return true
			}
			return e.ObjectOld.GetDeletionTimestamp().IsZero() && !e.ObjectNew.GetDeletionTimestamp().IsZero()
		},
	}
}

func agentHarnessRuntimePredicate(runtime v1alpha2.AgentHarnessRuntime) predicate.Predicate {
	primary := agentHarnessPrimaryPredicate()
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return primary.Create(e) && agentHarnessObjectMatchesRuntime(e.Object, runtime)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return primary.Delete(e) && agentHarnessObjectMatchesRuntime(e.Object, runtime)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return primary.Update(e) &&
				(agentHarnessObjectMatchesRuntime(e.ObjectOld, runtime) || agentHarnessObjectMatchesRuntime(e.ObjectNew, runtime))
		},
	}
}

func agentHarnessObjectMatchesRuntime(obj client.Object, runtime v1alpha2.AgentHarnessRuntime) bool {
	ah, ok := obj.(*v1alpha2.AgentHarness)
	if !ok || ah == nil {
		return false
	}
	return effectiveAgentHarnessRuntime(ah) == runtime
}
