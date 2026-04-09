/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

var (
	agentControllerLog = ctrl.Log.WithName("agent-controller")
)

// AgentController reconciles a Agent object
type AgentController struct {
	Scheme        *runtime.Scheme
	Reconciler    reconciler.KagentReconciler
	AdkTranslator agent_translator.AdkApiTranslator
}

// +kubebuilder:rbac:groups=kagent.dev,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=agents/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes/finalizers,verbs=update

func (r *AgentController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	return ctrl.Result{}, r.Reconciler.ReconcileKagentAgent(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentController) SetupWithManager(mgr ctrl.Manager) error {
	build := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.Agent{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})))

	var err error
	build, err = addOwnedResourceWatches(build, mgr, r.AdkTranslator.GetOwnedResourceTypes())
	if err != nil {
		return err
	}
	build, err = addCommonAgentWatches(build, mgr, agentWatchFinders{
		modelConfig:     r.agentDependencyFinder("failed to list Agents in order to reconcile ModelConfig update", usesModelConfig),
		remoteMCPServer: r.agentDependencyFinder("failed to list Agents in order to reconcile ToolServer update", usesRemoteMCPServer),
		mcpService:      r.agentDependencyFinder("failed to list agents in order to reconcile MCPService update", usesMCPService),
		configMap:       r.agentDependencyFinder("failed to list agents in order to reconcile ConfigMap update", referencesConfigMap),
		mcpServer:       r.agentDependencyFinder("failed to list agents in order to reconcile MCPServer update", usesMCPServer),
	})
	if err != nil {
		return err
	}

	return build.Named("agent").Complete(r)
}

func (r *AgentController) agentDependencyFinder(errMsg string, pred agentDependencyPredicate) dependentRefFinder {
	return func(ctx context.Context, cl client.Client, obj types.NamespacedName) []types.NamespacedName {
		var agentsList v1alpha2.AgentList
		if err := cl.List(ctx, &agentsList); err != nil {
			agentControllerLog.Error(err, errMsg)
			return nil
		}

		return collectAgentRefs(agentsList.Items, func(agent v1alpha2.AgentObject) bool {
			return pred(agent, obj)
		})
	}
}

type ownedObjectPredicate = typedOwnedObjectPredicate[client.Object]

type typedOwnedObjectPredicate[object metav1.Object] struct {
	predicate.TypedFuncs[object]
}

// Create implements default CreateEvent filter to ignore creation events for
// owned objects as this controller most likely created it and does not need to
// re-reconcile.
func (typedOwnedObjectPredicate[object]) Create(e event.TypedCreateEvent[object]) bool {
	return false
}
