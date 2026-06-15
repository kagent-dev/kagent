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

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

const (
	// substrateDeleteTimeout is the maximum time to wait for substrate cleanup during delete.
	substrateDeleteTimeout = 5 * time.Minute
)

// +kubebuilder:rbac:groups=ate.dev,resources=workerpools,verbs=get;list;watch
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates/status,verbs=get

// SubstrateAgentHarnessController reconciles AgentHarness resources that use the
// Substrate runtime.
type SubstrateAgentHarnessController struct {
	Client             client.Client
	Recorder           events.EventRecorder
	OpenClawBackend    sandboxbackend.AsyncBackend
	NemoClawBackend    sandboxbackend.AsyncBackend
	SubstrateLifecycle substrate.AgentHarnessLifecycle
}

func (r *SubstrateAgentHarnessController) backendFor(ah *v1alpha2.AgentHarness) sandboxbackend.AsyncBackend {
	switch ah.Spec.Backend {
	case v1alpha2.AgentHarnessBackendOpenClaw:
		return r.OpenClawBackend
	case v1alpha2.AgentHarnessBackendNemoClaw:
		return r.NemoClawBackend
	default:
		return nil
	}
}

func (r *SubstrateAgentHarnessController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("agentHarness", req.NamespacedName)

	var ah v1alpha2.AgentHarness
	if err := r.Client.Get(ctx, req.NamespacedName, &ah); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get AgentHarness: %w", err)
	}
	if effectiveAgentHarnessRuntime(&ah) != v1alpha2.AgentHarnessRuntimeSubstrate {
		return ctrl.Result{}, nil
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

	backend := r.backendFor(&ah)
	if backend == nil {
		return reconcileBackendUnavailable(ctx, r.Client, &ah, v1alpha2.AgentHarnessRuntimeSubstrate)
	}

	lifecycleState, err := r.SubstrateLifecycle.EnsureGeneratedTemplate(ctx, &ah)
	if err != nil {
		log.Error(err, "substrate lifecycle reconciliation failed")
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
			"SubstrateLifecycleFailed", err.Error())
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"SubstrateLifecycleFailed", "")
		if perr := patchAgentHarnessStatus(ctx, r.Client, &ah); perr != nil {
			return ctrl.Result{}, perr
		}
		return ctrl.Result{}, err
	}
	if lifecycleState.ActorTemplateReady {
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeActorTemplateReady,
			metav1.ConditionTrue, "Ready", "ActorTemplate golden snapshot is ready")
	} else {
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeActorTemplateReady,
			metav1.ConditionFalse, "NotReady", "waiting for ActorTemplate golden snapshot")
	}
	if err := patchAgentHarnessStatus(ctx, r.Client, &ah); err != nil {
		return ctrl.Result{}, err
	}
	if !lifecycleState.ActorTemplateReady {
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionTrue,
			"SubstrateLifecyclePending", "waiting for ActorTemplate golden snapshot")
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeActorReady, metav1.ConditionFalse,
			"ActorNotCreated", "waiting for ActorTemplate before creating actor")
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"ActorTemplateNotReady", "ActorTemplate is not Ready yet")
		if err := patchAgentHarnessStatus(ctx, r.Client, &ah); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
	}
	if err := r.Client.Get(ctx, req.NamespacedName, &ah); err != nil {
		return ctrl.Result{}, fmt.Errorf("reload AgentHarness after substrate lifecycle reconciliation: %w", err)
	}

	return r.reconcileBackend(ctx, req, &ah, backend, log)
}

func (r *SubstrateAgentHarnessController) reconcileBackend(ctx context.Context, req ctrl.Request, ah *v1alpha2.AgentHarness, backend sandboxbackend.AsyncBackend, log logr.Logger) (ctrl.Result, error) {
	res, err := backend.EnsureAgentHarness(ctx, ah)
	if err != nil {
		log.Error(err, "EnsureAgentHarness failed")
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
			"EnsureFailed", err.Error())
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"EnsureFailed", err.Error())
		if perr := patchAgentHarnessStatus(ctx, r.Client, ah); perr != nil {
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
	setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionTrue,
		"AgentHarnessAccepted", "backend accepted sandbox request")

	st, reason, msg := backend.GetStatus(ctx, res.Handle)
	pending := postReadyBootstrapPending(ah)
	if st == metav1.ConditionTrue && pending {
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeActorReady, st, reason, msg)
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeBootstrapReady, metav1.ConditionFalse,
			"BootstrapPending",
			"waiting for post-ready bootstrap (OnAgentHarnessReady) to finish")
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"BootstrapPending",
			"gateway sandbox is ready; waiting for post-ready bootstrap (OnAgentHarnessReady) to finish")
	} else {
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeActorReady, st, reason, msg)
		if pending {
			setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeBootstrapReady, metav1.ConditionFalse,
				"ActorNotReady", "waiting for actor before post-ready bootstrap")
		}
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeReady, st, reason, msg)
	}
	ah.Status.ObservedGeneration = ah.Generation

	if err := patchAgentHarnessStatus(ctx, r.Client, ah); err != nil {
		return ctrl.Result{}, err
	}

	if st != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
	}
	if pending {
		if err := maybePostReadyBootstrap(ctx, client.ObjectKeyFromObject(ah), ah, res.Handle, backend); err != nil {
			log.Error(err, "post-ready sandbox bootstrap failed")
			return ctrl.Result{}, err
		}
		var latest v1alpha2.AgentHarness
		if err := r.Client.Get(ctx, req.NamespacedName, &latest); err != nil {
			return ctrl.Result{}, fmt.Errorf("get AgentHarness after bootstrap: %w", err)
		}
		st2, reason2, msg2 := backend.GetStatus(ctx, res.Handle)
		setAgentHarnessCondition(&latest, v1alpha2.AgentHarnessConditionTypeActorReady, st2, reason2, msg2)
		setAgentHarnessCondition(&latest, v1alpha2.AgentHarnessConditionTypeBootstrapReady, metav1.ConditionTrue,
			"BootstrapComplete", "post-ready bootstrap completed")
		setAgentHarnessCondition(&latest, v1alpha2.AgentHarnessConditionTypeReady, st2, reason2, msg2)
		latest.Status.ObservedGeneration = latest.Generation
		if err := r.Client.Status().Update(ctx, &latest); err != nil {
			return ctrl.Result{}, fmt.Errorf("update AgentHarness status after bootstrap: %w", err)
		}
	}
	return ctrl.Result{}, nil
}

func (r *SubstrateAgentHarnessController) reconcileDelete(ctx context.Context, ah *v1alpha2.AgentHarness) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ah, agentHarnessFinalizer) {
		return ctrl.Result{}, nil
	}

	if substrateDeleteTimedOut(ah) {
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeReady,
			metav1.ConditionFalse, "DeleteTimeout", "substrate cleanup exceeded timeout")
		if err := patchAgentHarnessStatus(ctx, r.Client, ah); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, fmt.Errorf("substrate cleanup timed out for AgentHarness %s", ah.Name)
	}

	if ah.Status.BackendRef != nil {
		actorID := ah.Status.BackendRef.ID
		if actorID != "" {
			backend := r.backendFor(ah)
			actorDone := true
			var err error
			if backend != nil {
				actorDone, err = backend.DeleteAgentHarness(ctx, sandboxbackend.Handle{ID: actorID})
			}
			if err != nil {
				if r.Recorder != nil {
					r.Recorder.Eventf(ah, nil, "Warning", "AgentHarnessDeleteFailed", "DeleteAgentHarness", "%s", err.Error())
				}
				return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
			}
			if !actorDone {
				setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeActorReady,
					metav1.ConditionFalse, "ActorDeleting", fmt.Sprintf("waiting for substrate actor %q deletion", actorID))
				if err := patchAgentHarnessStatus(ctx, r.Client, ah); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
			}
		}
		ah.Status.BackendRef = nil
		if err := patchAgentHarnessStatus(ctx, r.Client, ah); err != nil {
			return ctrl.Result{}, err
		}
	}

	complete, err := r.SubstrateLifecycle.CleanupGeneratedTemplate(ctx, ah)
	if err != nil {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, fmt.Errorf("cleanup substrate lifecycle: %w", err)
	}
	if !complete {
		setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeActorTemplateReady,
			metav1.ConditionFalse, "GoldenActorDeleting", "waiting for generated ActorTemplate golden actor deletion")
		if err := patchAgentHarnessStatus(ctx, r.Client, ah); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
	}
	setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeActorTemplateReady,
		metav1.ConditionFalse, "Deleting", "generated ActorTemplate will be garbage collected")
	if err := patchAgentHarnessStatus(ctx, r.Client, ah); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(ah, agentHarnessFinalizer)
	if err := r.Client.Update(ctx, ah); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func substrateDeleteTimedOut(ah *v1alpha2.AgentHarness) bool {
	if ah == nil || ah.DeletionTimestamp.IsZero() {
		return false
	}
	return time.Since(ah.DeletionTimestamp.Time) > substrateDeleteTimeout
}

// SetupWithManager registers the Substrate AgentHarness controller with the manager.
func (r *SubstrateAgentHarnessController) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&v1alpha2.AgentHarness{}, builder.WithPredicates(agentHarnessRuntimePredicate(v1alpha2.AgentHarnessRuntimeSubstrate)))
	b = r.substrateWatches(b)
	return b.Named("agentharness-substrate").Complete(r)
}
