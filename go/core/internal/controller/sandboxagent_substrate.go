package controller

import (
	"context"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

const sandboxAgentSubstrateFinalizer = "kagent.dev/sandbox-agent-substrate-cleanup"

// substrateConfigured reports whether the substrate backend is wired. The lifecycle and actor
// backend are constructed together (only when an ate-api endpoint is set), so they are
// all-or-nothing; gating once here lets the substrate reconcile path and its helpers assume both
// are present rather than nil-checking each dependency at every call site.
func (r *SandboxAgentController) substrateConfigured() bool {
	return r.SubstrateLifecycle != nil && r.SubstrateActorBackend != nil
}

// reconcileSubstrateSandboxAgent is only reached when substrateConfigured() is true (see
// Reconcile), so SubstrateLifecycle and SubstrateActorBackend are guaranteed non-nil here and in
// the helpers it calls.
func (r *SandboxAgentController) reconcileSubstrateSandboxAgent(ctx context.Context, sa *v1alpha2.SandboxAgent) (ctrl.Result, error) {
	if !sa.DeletionTimestamp.IsZero() {
		return r.reconcileSubstrateSandboxAgentDelete(ctx, sa)
	}
	if controllerutil.AddFinalizer(sa, sandboxAgentSubstrateFinalizer) {
		if err := r.Client.Update(ctx, sa); err != nil {
			return ctrl.Result{}, fmt.Errorf("add substrate finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SandboxAgentController) reconcileSubstrateSandboxAgentDelete(ctx context.Context, sa *v1alpha2.SandboxAgent) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(sa, sandboxAgentSubstrateFinalizer) {
		return ctrl.Result{}, nil
	}
	if substrateSandboxAgentDeleteTimedOut(sa) {
		sandboxAgentControllerLog.Info(
			"substrate cleanup timed out; removing finalizer so SandboxAgent can be deleted",
			"sandboxagent", sa.Namespace+"/"+sa.Name,
		)
		return r.removeSubstrateSandboxAgentFinalizer(ctx, sa)
	}

	if done, err := r.SubstrateActorBackend.DeleteAllSandboxAgentActors(ctx, sa); err != nil {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
	} else if !done {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
	}

	if done, err := r.SubstrateLifecycle.CleanupSandboxAgentTemplate(ctx, sa); err != nil {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, err
	} else if !done {
		return ctrl.Result{RequeueAfter: agentHarnessNotReadyRequeue}, nil
	}

	return r.removeSubstrateSandboxAgentFinalizer(ctx, sa)
}

func (r *SandboxAgentController) removeSubstrateSandboxAgentFinalizer(ctx context.Context, sa *v1alpha2.SandboxAgent) (ctrl.Result, error) {
	controllerutil.RemoveFinalizer(sa, sandboxAgentSubstrateFinalizer)
	if err := r.Client.Update(ctx, sa); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove substrate finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func substrateSandboxAgentDeleteTimedOut(sa *v1alpha2.SandboxAgent) bool {
	if sa == nil || sa.DeletionTimestamp.IsZero() {
		return false
	}
	return time.Since(sa.DeletionTimestamp.Time) > substrateDeleteTimeout
}

func (r *SandboxAgentController) enqueueSandboxAgentForSubstrateResource(_ context.Context, obj client.Object) []reconcile.Request {
	agentName := substrate.SandboxAgentNameFromLabels(obj.GetLabels())
	if agentName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      agentName,
		},
	}}
}
