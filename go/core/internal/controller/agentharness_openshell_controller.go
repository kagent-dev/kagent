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
)

// OpenShellAgentHarnessController reconciles AgentHarness resources that use the
// OpenShell runtime.
type OpenShellAgentHarnessController struct {
	Client          client.Client
	Recorder        events.EventRecorder
	OpenClawBackend sandboxbackend.AsyncBackend
	HermesBackend   sandboxbackend.AsyncBackend
}

func (r *OpenShellAgentHarnessController) backendFor(ah *v1alpha2.AgentHarness) sandboxbackend.AsyncBackend {
	switch ah.Spec.Backend {
	case v1alpha2.AgentHarnessBackendOpenClaw, v1alpha2.AgentHarnessBackendNemoClaw:
		return r.OpenClawBackend
	case v1alpha2.AgentHarnessBackendHermes:
		return r.HermesBackend
	default:
		return nil
	}
}

func (r *OpenShellAgentHarnessController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("agentHarness", req.NamespacedName)

	var ah v1alpha2.AgentHarness
	if err := r.Client.Get(ctx, req.NamespacedName, &ah); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get AgentHarness: %w", err)
	}
	if effectiveAgentHarnessRuntime(&ah) != v1alpha2.AgentHarnessRuntimeOpenshell {
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
		return reconcileBackendUnavailable(ctx, r.Client, &ah, v1alpha2.AgentHarnessRuntimeOpenshell)
	}

	return r.reconcileBackend(ctx, req, &ah, backend, log)
}

func (r *OpenShellAgentHarnessController) reconcileBackend(ctx context.Context, req ctrl.Request, ah *v1alpha2.AgentHarness, backend sandboxbackend.AsyncBackend, log logr.Logger) (ctrl.Result, error) {
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

func (r *OpenShellAgentHarnessController) reconcileDelete(ctx context.Context, ah *v1alpha2.AgentHarness) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ah, agentHarnessFinalizer) {
		return ctrl.Result{}, nil
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
					metav1.ConditionFalse, "ActorDeleting", fmt.Sprintf("waiting for backend actor %q deletion", actorID))
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

	controllerutil.RemoveFinalizer(ah, agentHarnessFinalizer)
	if err := r.Client.Update(ctx, ah); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the OpenShell AgentHarness controller with the manager.
func (r *OpenShellAgentHarnessController) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&v1alpha2.AgentHarness{}, builder.WithPredicates(agentHarnessRuntimePredicate(v1alpha2.AgentHarnessRuntimeOpenshell)))
	return b.Named("agentharness-openshell").Complete(r)
}
