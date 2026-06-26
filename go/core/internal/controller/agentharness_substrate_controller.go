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
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

const (
	// substrateDeleteTimeout is the maximum time to wait for substrate cleanup during delete.
	substrateDeleteTimeout = 5 * time.Minute

	// agentHarnessDBType is the database.Agent.Type recorded for AgentHarness
	// rows. It distinguishes harnesses from deployment/sandbox agents in the
	// shared agent table.
	agentHarnessDBType = "AgentHarness"
)

// +kubebuilder:rbac:groups=ate.dev,resources=workerpools,verbs=get;list;watch
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates/status,verbs=get

// AgentHarnessSessionActorCleaner deletes the substrate actors spun from an
// AgentHarness's generated ActorTemplate. Implemented by
// *substrate.AgentHarnessSessionActorBackend.
type AgentHarnessSessionActorCleaner interface {
	DeleteAllAgentHarnessActors(ctx context.Context, ah *v1alpha2.AgentHarness) (bool, error)
}

// SubstrateAgentHarnessController reconciles AgentHarness resources that use the
// Substrate runtime.
type SubstrateAgentHarnessController struct {
	Client   client.Client
	Recorder events.EventRecorder
	// Backends maps the harness backend type to its substrate AsyncBackend.
	Backends           map[v1alpha2.AgentHarnessBackendType]sandboxbackend.AsyncBackend
	SubstrateLifecycle substrate.AgentHarnessLifecycle
	// SessionActorBackend manages the shared actor spun from the harness's
	// generated ActorTemplate. The controller uses it only to clean up actors
	// on delete; the shared actor is created on demand by the HTTP gateway on
	// the first chat connect.
	SessionActorBackend AgentHarnessSessionActorCleaner
	// DbClient records each AgentHarness as a row in the shared agent table so
	// that chat sessions resolve through the same agent lookup as regular
	// agents. The row is upserted once the harness is Ready and removed on
	// delete.
	DbClient database.Client
}

func (r *SubstrateAgentHarnessController) backendFor(ah *v1alpha2.AgentHarness) sandboxbackend.AsyncBackend {
	return r.Backends[ah.Spec.Backend]
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
		return reconcileBackendUnavailable(ctx, r.Client, &ah)
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

	// The AgentHarness is a template: once its generated ActorTemplate golden
	// snapshot is Ready, the harness is Ready. We do NOT create an actor here.
	// One shared actor is spun from the template per harness, created on demand
	// by the HTTP gateway (AgentHarnessSessionActorBackend) on the first chat
	// connect. Every chat is multiplexed as an ACP session inside that single
	// actor's long-lived child process; the actor id is keyed on the harness
	// (namespace/name), not the session.
	if err := r.upsertHarnessAgentRow(ctx, &ah); err != nil {
		return ctrl.Result{}, err
	}
	setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionTrue,
		"AgentHarnessAccepted", "ActorTemplate golden snapshot is ready")
	setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeActorReady, metav1.ConditionTrue,
		"TemplateReady", "shared actor is created on demand on the first chat connect")
	setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionTrue,
		"TemplateReady", "AgentHarness template is ready; one shared actor serves all chat sessions")
	ah.Status.ObservedGeneration = ah.Generation
	if err := patchAgentHarnessStatus(ctx, r.Client, &ah); err != nil {
		return ctrl.Result{}, err
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

	// Delete every actor belonging to this harness: the legacy single actor (if
	// any harness still has one recorded) plus all per-session actors spun from
	// the generated ActorTemplate.
	if r.SessionActorBackend != nil {
		actorsDone, err := r.SessionActorBackend.DeleteAllAgentHarnessActors(ctx, ah)
		if err != nil {
			if r.Recorder != nil {
				r.Recorder.Eventf(ah, nil, "Warning", "AgentHarnessDeleteFailed", "DeleteAgentHarnessActors", "%s", err.Error())
			}
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
		}
		if !actorsDone {
			setAgentHarnessCondition(ah, v1alpha2.AgentHarnessConditionTypeActorReady,
				metav1.ConditionFalse, "ActorDeleting", "waiting for substrate session actors deletion")
			if err := patchAgentHarnessStatus(ctx, r.Client, ah); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
		}
	}
	if ah.Status.BackendRef != nil {
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

	if err := r.deleteHarnessAgentRow(ctx, ah); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(ah, agentHarnessFinalizer)
	if err := r.Client.Update(ctx, ah); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// upsertHarnessAgentRow records the AgentHarness as a row in the shared agent
// table, keyed by the same python-identifier used for chat sessions. This lets
// session handlers resolve harness chats through the regular agent lookup
// instead of a harness-specific fallback. It is a no-op when no DB client is
// configured.
func (r *SubstrateAgentHarnessController) upsertHarnessAgentRow(ctx context.Context, ah *v1alpha2.AgentHarness) error {
	if r.DbClient == nil {
		return nil
	}
	id := utils.ConvertToPythonIdentifier(utils.GetObjectRef(ah))
	dbAgent := &database.Agent{
		ID:   id,
		Type: agentHarnessDBType,
		// Harnesses are not sandbox agents: they serve many concurrent chats
		// from one shared actor, so they must not pick up the sandbox
		// single-session restriction.
		WorkloadType: v1alpha2.WorkloadModeDeployment,
	}
	if err := r.DbClient.StoreAgent(ctx, dbAgent); err != nil {
		return fmt.Errorf("store agent row for AgentHarness %s: %w", id, err)
	}
	return nil
}

// deleteHarnessAgentRow removes the AgentHarness's row from the shared agent
// table. It is a no-op when no DB client is configured.
func (r *SubstrateAgentHarnessController) deleteHarnessAgentRow(ctx context.Context, ah *v1alpha2.AgentHarness) error {
	if r.DbClient == nil {
		return nil
	}
	id := utils.ConvertToPythonIdentifier(utils.GetObjectRef(ah))
	if err := r.DbClient.DeleteAgent(ctx, id); err != nil {
		return fmt.Errorf("delete agent row for AgentHarness %s: %w", id, err)
	}
	return nil
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
		For(&v1alpha2.AgentHarness{}, builder.WithPredicates(agentHarnessPrimaryPredicate()))
	b = r.substrateWatches(b)
	return b.Named("agentharness-substrate").Complete(r)
}
