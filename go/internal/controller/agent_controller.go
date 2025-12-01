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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/reconciler"
	agent_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
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

func (r *AgentController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	return ctrl.Result{}, r.Reconciler.ReconcileKagentAgent(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentController) SetupWithManager(mgr ctrl.Manager) error {
	build := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha2.Agent{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})))

	// Setup owns relationships for resources created by the Agent controller -
	// for now ownership of agent resources is handled by the ADK translator
	for _, ownedType := range r.AdkTranslator.GetOwnedResourceTypes() {
		build = build.Owns(ownedType, builder.WithPredicates(ownedObjectPredicate{}, predicate.ResourceVersionChangedPredicate{}))
	}

	// Setup watches for secondary resources that are not owned by the Agent
	build = build.Watches(
		&v1alpha2.ModelConfig{},
		handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			requests := []reconcile.Request{}

			for _, agent := range r.findAgentsUsingModelConfig(ctx, mgr.GetClient(), types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			}) {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      agent.ObjectMeta.Name,
						Namespace: agent.ObjectMeta.Namespace,
					},
				})
			}

			return requests
		}),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	).
		Watches(
			&v1alpha2.RemoteMCPServer{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				for _, agent := range r.findAgentsUsingRemoteMCPServer(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      agent.ObjectMeta.Name,
							Namespace: agent.ObjectMeta.Namespace,
						},
					})
				}

				return requests
			}),
		).
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				for _, agent := range r.findAgentsUsingMCPService(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      agent.Name,
							Namespace: agent.Namespace,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		)

	if _, err := mgr.GetRESTMapper().RESTMapping(mcpServerGK); err == nil {
		build = build.Watches(
			&v1alpha1.MCPServer{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				for _, agent := range r.findAgentsUsingMCPServer(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      agent.Name,
							Namespace: agent.Namespace,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		)
	}

	return build.Named("agent").Complete(r)
}

func (r *AgentController) findAgentsUsingMCPServer(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.Agent {
	var agentsList v1alpha2.AgentList
	if err := cl.List(
		ctx,
		&agentsList,
	); err != nil {
		agentControllerLog.Error(err, "failed to list agents in order to reconcile MCPServer update")
		return nil
	}

	var agents []*v1alpha2.Agent
	for _, agent := range agentsList.Items {
		if agent.Namespace != obj.Namespace {
			continue
		}

		if agent.Spec.Type != v1alpha2.AgentType_Declarative {
			continue
		}

		for _, tool := range agent.Spec.Declarative.Tools {
			if tool.McpServer == nil {
				continue
			}

			if tool.McpServer.ApiGroup != "kagent.dev" || tool.McpServer.Kind != "MCPServer" {
				continue
			}

			if tool.McpServer.Name == obj.Name {
				agents = append(agents, &agent)
			}
		}

	}

	return agents
}

func (r *AgentController) findAgentsUsingRemoteMCPServer(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.Agent {
	var agents []*v1alpha2.Agent

	var agentsList v1alpha2.AgentList
	if err := cl.List(
		ctx,
		&agentsList,
	); err != nil {
		agentControllerLog.Error(err, "failed to list Agents in order to reconcile ToolServer update")
		return agents
	}

	appendAgentIfUsesRemoteMCPServer := func(agent *v1alpha2.Agent) {
		if agent.Spec.Type != v1alpha2.AgentType_Declarative {
			return
		}

		for _, tool := range agent.Spec.Declarative.Tools {
			if tool.McpServer == nil {
				return
			}

			if agent.Namespace != obj.Namespace {
				continue
			}

			if tool.McpServer.Name == obj.Name {
				agents = append(agents, agent)
				return
			}
		}
	}

	for _, agent := range agentsList.Items {
		agent := agent
		appendAgentIfUsesRemoteMCPServer(&agent)
	}

	return agents
}

func (r *AgentController) findAgentsUsingMCPService(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.Agent {

	var agentsList v1alpha2.AgentList
	if err := cl.List(
		ctx,
		&agentsList,
	); err != nil {
		agentControllerLog.Error(err, "failed to list agents in order to reconcile MCPService update")
		return nil
	}

	var agents []*v1alpha2.Agent
	for _, agent := range agentsList.Items {
		if agent.Namespace != obj.Namespace {
			continue
		}

		if agent.Spec.Type != v1alpha2.AgentType_Declarative {
			continue
		}

		for _, tool := range agent.Spec.Declarative.Tools {
			if tool.McpServer == nil {
				continue
			}

			if tool.McpServer.ApiGroup != "" || tool.McpServer.Kind != "Service" {
				continue
			}

			if tool.McpServer.Name == obj.Name {
				agents = append(agents, &agent)
			}
		}
	}

	return agents
}

func (r *AgentController) findAgentsUsingModelConfig(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.Agent {
	var agents []*v1alpha2.Agent

	var agentsList v1alpha2.AgentList
	if err := cl.List(
		ctx,
		&agentsList,
	); err != nil {
		agentControllerLog.Error(err, "failed to list Agents in order to reconcile ModelConfig update")
		return agents
	}

	for i := range agentsList.Items {
		agent := &agentsList.Items[i]
		// Must be in the same namespace as the model config
		if agent.Namespace != obj.Namespace {
			continue
		}

		if agent.Spec.Type != v1alpha2.AgentType_Declarative {
			continue
		}

		if agent.Spec.Declarative.ModelConfig == obj.Name {
			agents = append(agents, agent)
		}

	}

	return agents
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
