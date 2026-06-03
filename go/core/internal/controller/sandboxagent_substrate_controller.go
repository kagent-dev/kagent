/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
*/

package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

const sandboxAgentSubstrateFinalizer = "kagent.dev/sandbox-agent-substrate-cleanup"

// SubstrateSandboxAgentController manages ate-api actors for SandboxAgent resources on Agent Substrate.
type SubstrateSandboxAgentController struct {
	Client       client.Client
	ActorBackend *substrate.SandboxAgentActorBackend
	Lifecycle    *substrate.Lifecycle
}

func (r *SubstrateSandboxAgentController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sa v1alpha2.SandboxAgent
	if err := r.Client.Get(ctx, req.NamespacedName, &sa); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SandboxAgent: %w", err)
	}
	if v1alpha2.AgentSandboxPlatform(&sa.Spec) != v1alpha2.SandboxPlatformSubstrate {
		return ctrl.Result{}, nil
	}

	if !sa.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &sa)
	}

	if controllerutil.AddFinalizer(&sa, sandboxAgentSubstrateFinalizer) {
		if err := r.Client.Update(ctx, &sa); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if r.ActorBackend == nil {
		return ctrl.Result{}, fmt.Errorf("substrate sandbox agent actor backend is not configured")
	}

	tmplKey := client.ObjectKey{Namespace: sa.Namespace, Name: substrate.SandboxAgentActorTemplateName(&sa)}
	ready, err := r.Lifecycle.ActorTemplateReady(ctx, tmplKey)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ready {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
	}

	sa.Status.ObservedGeneration = sa.Generation
	if err := r.Client.Status().Update(ctx, &sa); err != nil {
		return ctrl.Result{}, fmt.Errorf("update SandboxAgent substrate status: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *SubstrateSandboxAgentController) reconcileDelete(ctx context.Context, sa *v1alpha2.SandboxAgent) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sa, sandboxAgentSubstrateFinalizer) {
		return ctrl.Result{}, nil
	}
	if substrateDeleteTimedOutForSandboxAgent(sa) {
		return ctrl.Result{}, fmt.Errorf("substrate cleanup timed out for SandboxAgent %s", sa.Name)
	}

	if r.ActorBackend != nil {
		done, err := r.ActorBackend.DeleteAllSandboxAgentActors(ctx, sa)
		if err != nil {
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
		}
		if !done {
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
		}
	}
	if sa.Status.Substrate != nil && sa.Status.Substrate.ActorID != "" {
		sa.Status.Substrate.ActorID = ""
		if err := r.Client.Status().Update(ctx, sa); err != nil {
			return ctrl.Result{}, err
		}
	}

	if r.Lifecycle != nil {
		done, err := r.Lifecycle.CleanupSandboxAgentTemplate(ctx, sa)
		if err != nil {
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
		}
		if !done {
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
		}
	}

	controllerutil.RemoveFinalizer(sa, sandboxAgentSubstrateFinalizer)
	if err := r.Client.Update(ctx, sa); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func substrateDeleteTimedOutForSandboxAgent(sa *v1alpha2.SandboxAgent) bool {
	if sa == nil || sa.DeletionTimestamp.IsZero() {
		return false
	}
	return time.Since(sa.DeletionTimestamp.Time) > substrateDeleteTimeout
}

func sandboxAgentSubstratePredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		sa, ok := obj.(*v1alpha2.SandboxAgent)
		if !ok || sa == nil {
			return false
		}
		return v1alpha2.AgentSandboxPlatform(&sa.Spec) == v1alpha2.SandboxPlatformSubstrate
	})
}

// SetupWithManager registers the Substrate SandboxAgent controller.
func (r *SubstrateSandboxAgentController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&v1alpha2.SandboxAgent{}, builder.WithPredicates(sandboxAgentSubstratePredicate())).
		Named("sandboxagent-substrate").
		Complete(r)
}
