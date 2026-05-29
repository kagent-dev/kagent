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
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

const (
	// agentHarnessFinalizer guarantees the backend sandbox is deleted before the
	// Kubernetes object is removed.
	agentHarnessFinalizer = "kagent.dev/agent-harness-backend-cleanup"

	// agentHarnessNotReadyRequeue is how long we wait before re-polling backend
	// status while the sandbox is still provisioning.
	agentHarnessNotReadyRequeue = 10 * time.Second

	// substrateDeleteTimeout is the maximum time to wait for substrate cleanup during delete.
	substrateDeleteTimeout = 5 * time.Minute

	// annotationAgentHarnessBootstrapGeneration records the AgentHarness metadata.generation for which
	// post-ready bootstrap (backend OnAgentHarnessReady, e.g. exec hooks) already completed.
	annotationAgentHarnessBootstrapGeneration = "kagent.dev/agent-harness-bootstrap-generation"
)

// AgentHarnessController reconciles a kagent.dev/v1alpha2 AgentHarness against an
// AsyncBackend. It is intentionally independent of the SandboxAgent path —
// harness VMs are a generic exec/SSH-able environment with no in-cluster
// workload owned by kagent.
type AgentHarnessController struct {
	Client               client.Client
	Recorder             events.EventRecorder
	OpenshellBackends    map[v1alpha2.AgentHarnessBackendType]sandboxbackend.AsyncBackend
	SubstrateBackends    map[v1alpha2.AgentHarnessBackendType]sandboxbackend.AsyncBackend
	SubstrateProvisioner *substrate.Provisioner
}

func (r *AgentHarnessController) backendFor(ah *v1alpha2.AgentHarness) sandboxbackend.AsyncBackend {
	runtime := ah.Spec.Runtime
	if runtime == "" {
		runtime = v1alpha2.AgentHarnessRuntimeOpenshell
	}
	switch runtime {
	case v1alpha2.AgentHarnessRuntimeSubstrate:
		if r.SubstrateBackends == nil {
			return nil
		}
		return r.SubstrateBackends[ah.Spec.Backend]
	default:
		if r.OpenshellBackends == nil {
			return nil
		}
		return r.OpenshellBackends[ah.Spec.Backend]
	}
}

// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=agentharnesses/finalizers,verbs=update
// +kubebuilder:rbac:groups=ate.dev,resources=workerpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates/status,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

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

	backend := r.backendFor(&ah)
	if backend == nil {
		runtime := ah.Spec.Runtime
		if runtime == "" {
			runtime = v1alpha2.AgentHarnessRuntimeOpenshell
		}
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
			"BackendUnavailable",
			fmt.Sprintf("no %s backend configured for %q", runtime, ah.Spec.Backend))
		setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
			"BackendUnavailable", "")
		if err := r.patchAgentHarnessStatus(ctx, &ah); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	runtime := effectiveAgentHarnessRuntime(&ah)
	if runtime == v1alpha2.AgentHarnessRuntimeSubstrate {
		if r.SubstrateProvisioner == nil {
			log.Error(nil, "substrate provisioner not configured")
			setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
				"SubstrateProvisionerUnavailable",
				"substrate runtime requires a configured substrate provisioner (set --substrate-ate-api-endpoint)")
			setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
				"SubstrateProvisionerUnavailable", "")
			if err := r.patchAgentHarnessStatus(ctx, &ah); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		provRes, err := r.SubstrateProvisioner.Ensure(ctx, &ah)
		if err != nil {
			log.Error(err, "substrate provision failed")
			setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse,
				"SubstrateProvisionFailed", err.Error())
			setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
				"SubstrateProvisionFailed", "")
			if perr := r.patchAgentHarnessStatus(ctx, &ah); perr != nil {
				return ctrl.Result{}, perr
			}
			return ctrl.Result{}, err
		}
		if provRes.ActorTemplateReady {
			setSubstrateCondition(&ah, v1alpha2.AgentHarnessSubstrateConditionTypeActorTemplateReady,
				metav1.ConditionTrue, "Ready", "ActorTemplate golden snapshot is ready")
		} else {
			setSubstrateCondition(&ah, v1alpha2.AgentHarnessSubstrateConditionTypeActorTemplateReady,
				metav1.ConditionFalse, "NotReady", "waiting for ActorTemplate golden snapshot")
		}
		// Persist status before metadata annotation patch (client Patch can refresh ah and drop in-memory status).
		if err := r.patchAgentHarnessStatus(ctx, &ah); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.patchAgentHarnessProvisionAnnotations(ctx, &ah, provRes); err != nil {
			return ctrl.Result{}, err
		}
		if !provRes.ActorTemplateReady {
			setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionTrue,
				"SubstrateProvisioning", "waiting for ActorTemplate golden snapshot")
			setAgentHarnessCondition(&ah, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse,
				"ActorTemplateNotReady", "ActorTemplate is not Ready yet")
			if err := r.patchAgentHarnessStatus(ctx, &ah); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
		}
		if err := r.Client.Get(ctx, req.NamespacedName, &ah); err != nil {
			return ctrl.Result{}, fmt.Errorf("reload AgentHarness after substrate provision: %w", err)
		}
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

	if substrateDeleteTimedOut(ah) {
		setSubstrateCondition(ah, v1alpha2.AgentHarnessSubstrateConditionTypeResourcesCleaned,
			metav1.ConditionFalse, "DeleteTimeout", "substrate cleanup exceeded timeout")
		if err := r.patchAgentHarnessStatus(ctx, ah); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, fmt.Errorf("substrate cleanup timed out for AgentHarness %s", ah.Name)
	}

	runtime := effectiveAgentHarnessRuntime(ah)
	actorID := ""
	if ah.Status.BackendRef != nil {
		actorID = ah.Status.BackendRef.ID
	}

	if actorID != "" {
		var actorDone bool
		var err error
		if runtime == v1alpha2.AgentHarnessRuntimeSubstrate && r.SubstrateProvisioner != nil {
			actorDone, err = r.SubstrateProvisioner.AdvanceActorDelete(ctx, actorID)
		} else if del := r.backendFor(ah); del != nil {
			err = del.DeleteAgentHarness(ctx, sandboxbackend.Handle{ID: actorID})
			actorDone = err == nil
		} else {
			actorDone = true
		}
		if err != nil {
			if r.Recorder != nil {
				r.Recorder.Eventf(ah, nil, "Warning", "AgentHarnessDeleteFailed", "DeleteAgentHarness", "%s", err.Error())
			}
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
		}
		if !actorDone {
			setSubstrateCondition(ah, v1alpha2.AgentHarnessSubstrateConditionTypeResourcesCleaned,
				metav1.ConditionFalse, "ActorDeleting", fmt.Sprintf("waiting for substrate actor %q deletion", actorID))
			if err := r.patchAgentHarnessStatus(ctx, ah); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
		}
		ah.Status.BackendRef = nil
		if err := r.patchAgentHarnessStatus(ctx, ah); err != nil {
			return ctrl.Result{}, err
		}
	}

	if runtime == v1alpha2.AgentHarnessRuntimeSubstrate {
		if r.SubstrateProvisioner == nil {
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue},
				fmt.Errorf("substrate provisioner is not configured")
		}
		complete, err := r.SubstrateProvisioner.AdvanceDelete(ctx, ah)
		if err != nil {
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, fmt.Errorf("delete substrate resources: %w", err)
		}
		if !complete {
			setSubstrateCondition(ah, v1alpha2.AgentHarnessSubstrateConditionTypeResourcesCleaned,
				metav1.ConditionFalse, "CleanupInProgress", "waiting for managed Substrate resources to be removed")
			if err := r.patchAgentHarnessStatus(ctx, ah); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
		}
		setSubstrateCondition(ah, v1alpha2.AgentHarnessSubstrateConditionTypeResourcesCleaned,
			metav1.ConditionTrue, "Cleaned", "managed Substrate resources removed")
		if err := r.patchAgentHarnessStatus(ctx, ah); err != nil {
			return ctrl.Result{}, err
		}
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

func (r *AgentHarnessController) patchAgentHarnessStatus(ctx context.Context, ah *v1alpha2.AgentHarness) error {
	if err := r.Client.Status().Update(ctx, ah); err != nil {
		return fmt.Errorf("update AgentHarness status: %w", err)
	}
	return nil
}

func (r *AgentHarnessController) patchAgentHarnessProvisionAnnotations(ctx context.Context, ah *v1alpha2.AgentHarness, prov substrate.EnsureResult) error {
	base := ah.DeepCopy()
	if ah.Annotations == nil {
		ah.Annotations = map[string]string{}
	}
	if prov.ManagedWorkerPool {
		ah.Annotations[substrate.AnnotationManagedWorkerPool] = "true"
	}
	if prov.ManagedActorTemplate {
		ah.Annotations[substrate.AnnotationManagedActorTemplate] = "true"
	}
	if err := r.Client.Patch(ctx, ah, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch AgentHarness substrate annotations: %w", err)
	}
	return nil
}

func effectiveAgentHarnessRuntime(ah *v1alpha2.AgentHarness) v1alpha2.AgentHarnessRuntime {
	if ah.Spec.Runtime == "" {
		return v1alpha2.AgentHarnessRuntimeOpenshell
	}
	return ah.Spec.Runtime
}

func setAgentHarnessCondition(ah *v1alpha2.AgentHarness, t string, s metav1.ConditionStatus, reason, msg string) {
	setConditions(&ah.Status.Conditions, ah.Generation, t, s, reason, msg)
}

func setSubstrateCondition(ah *v1alpha2.AgentHarness, t string, s metav1.ConditionStatus, reason, msg string) {
	if ah.Status.Substrate == nil {
		ah.Status.Substrate = &v1alpha2.AgentHarnessSubstrateStatus{}
	}
	setConditions(&ah.Status.Substrate.Conditions, ah.Generation, t, s, reason, msg)
}

func setConditions(conditions *[]metav1.Condition, generation int64, t string, s metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i := range *conditions {
		c := &(*conditions)[i]
		if c.Type != t {
			continue
		}
		if c.Status != s {
			c.LastTransitionTime = now
		}
		c.Status = s
		c.Reason = reason
		c.Message = msg
		c.ObservedGeneration = generation
		return
	}
	*conditions = append(*conditions, metav1.Condition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
		ObservedGeneration: generation,
	})
}

// SetupWithManager registers the controller with the manager.
func (r *AgentHarnessController) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&v1alpha2.AgentHarness{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		)))
	b = r.substrateWatches(b)
	return b.Named("agentharness").Complete(r)
}
