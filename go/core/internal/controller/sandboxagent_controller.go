/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"reflect"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

var (
	sandboxAgentControllerLog = ctrl.Log.WithName("sandboxagent-controller")
)

// SandboxAgentController reconciles SandboxAgent objects for both agent-sandbox and
// Agent Substrate platforms. Platform-specific workload objects are selected by the
// sandbox routing backend; substrate delete cleanup is handled in this controller.
type SandboxAgentController struct {
	Client                client.Client
	Scheme                *runtime.Scheme
	Reconciler            reconciler.KagentReconciler
	AdkTranslator         agent_translator.AdkApiTranslator
	SubstrateLifecycle    *substrate.Lifecycle
	SubstrateActorBackend *substrate.SandboxAgentActorBackend
}

// +kubebuilder:rbac:groups=kagent.dev,resources=sandboxagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=sandboxagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=sandboxagents/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates/status,verbs=get

func (r *SandboxAgentController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sa v1alpha2.SandboxAgent
	if err := r.Client.Get(ctx, req.NamespacedName, &sa); err != nil {
		if apierrors.IsNotFound(err) {
			if recErr := r.Reconciler.ReconcileKagentSandboxAgent(ctx, req); recErr != nil {
				return ctrl.Result{}, recErr
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SandboxAgent: %w", err)
	}

	if sandboxAgentUsesSubstrate(&sa) && r.SubstrateLifecycle != nil {
		if res, err := r.reconcileSubstrateSandboxAgent(ctx, &sa); err != nil || !res.IsZero() {
			return res, err
		}
	}

	if err := r.Reconciler.ReconcileKagentSandboxAgent(ctx, req); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxAgentController) SetupWithManager(mgr ctrl.Manager) error {
	if r.Client == nil {
		r.Client = mgr.GetClient()
	}

	build := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.SandboxAgent{}, builder.WithPredicates(sandboxAgentPrimaryPredicate()))

	var err error
	build, err = addOwnedResourceWatches(build, mgr, r.AdkTranslator.GetOwnedResourceTypes())
	if err != nil {
		return err
	}
	if r.SubstrateLifecycle != nil {
		build = build.Watches(
			&atev1alpha1.ActorTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueSandboxAgentForSubstrateResource),
		)
	}
	build, err = addCommonAgentWatches(build, mgr, agentWatchFinders{
		modelConfig:     r.sandboxAgentDependencyFinder("failed to list sandboxagents for ModelConfig watch", usesModelConfig),
		remoteMCPServer: r.sandboxAgentDependencyFinder("failed to list sandboxagents for RemoteMCPServer watch", usesRemoteMCPServer),
		mcpService:      r.sandboxAgentDependencyFinder("failed to list sandboxagents for Service watch", usesMCPService),
		configMap:       r.sandboxAgentDependencyFinder("failed to list sandboxagents for ConfigMap watch", referencesConfigMap),
		mcpServer:       r.sandboxAgentDependencyFinder("failed to list sandboxagents for MCPServer watch", usesMCPServer),
	})
	if err != nil {
		return err
	}

	return build.Named("sandboxagent").Complete(r)
}

func sandboxAgentPrimaryPredicate() predicate.Predicate {
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

func (r *SandboxAgentController) sandboxAgentDependencyFinder(errMsg string, pred agentDependencyPredicate) dependentRefFinder {
	return func(ctx context.Context, cl client.Client, obj types.NamespacedName) []types.NamespacedName {
		var list v1alpha2.SandboxAgentList
		if err := cl.List(ctx, &list); err != nil {
			sandboxAgentControllerLog.Error(err, errMsg)
			return nil
		}

		return collectSandboxAgentRefs(list.Items, func(agent v1alpha2.AgentObject) bool {
			return pred(agent, obj)
		})
	}
}
