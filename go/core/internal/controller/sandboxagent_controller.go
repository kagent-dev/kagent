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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
)

var (
	sandboxAgentControllerLog = ctrl.Log.WithName("sandboxagent-controller")
)

// SandboxAgentController reconciles SandboxAgent objects.
type SandboxAgentController struct {
	Scheme        *runtime.Scheme
	Reconciler    reconciler.KagentReconciler
	AdkTranslator agent_translator.AdkApiTranslator
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

func (r *SandboxAgentController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = ctrl.LoggerFrom(ctx)
	return ctrl.Result{}, r.Reconciler.ReconcileKagentSandboxAgent(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxAgentController) SetupWithManager(mgr ctrl.Manager) error {
	build := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.SandboxAgent{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})))

	for _, ownedType := range r.AdkTranslator.GetOwnedResourceTypes() {
		build = build.Owns(ownedType, builder.WithPredicates(ownedObjectPredicate{}, predicate.ResourceVersionChangedPredicate{}))
	}

	build = build.Watches(
		&v1alpha2.ModelConfig{},
		handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			var requests []reconcile.Request
			for _, sa := range r.findSandboxAgentsUsingModelConfig(ctx, mgr.GetClient(), types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			}) {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      sa.Name,
						Namespace: sa.Namespace,
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
				var requests []reconcile.Request
				for _, sa := range r.findSandboxAgentsUsingRemoteMCPServer(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      sa.Name,
							Namespace: sa.Namespace,
						},
					})
				}
				return requests
			}),
		).
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				var requests []reconcile.Request
				for _, sa := range r.findSandboxAgentsUsingMCPService(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      sa.Name,
							Namespace: sa.Namespace,
						},
					})
				}
				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				var requests []reconcile.Request
				for _, sa := range r.findSandboxAgentsReferencingConfigMap(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      sa.Name,
							Namespace: sa.Namespace,
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
				var requests []reconcile.Request
				for _, sa := range r.findSandboxAgentsUsingMCPServer(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      sa.Name,
							Namespace: sa.Namespace,
						},
					})
				}
				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		)
	}

	return build.Named("sandboxagent").Complete(r)
}

func (r *SandboxAgentController) findSandboxAgentsUsingMCPServer(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.SandboxAgent {
	var list v1alpha2.SandboxAgentList
	if err := cl.List(ctx, &list); err != nil {
		sandboxAgentControllerLog.Error(err, "failed to list sandboxagents for MCPServer watch")
		return nil
	}

	var out []*v1alpha2.SandboxAgent
	for i := range list.Items {
		sa := &list.Items[i]
		decl := sa.Spec.Declarative
		if decl == nil {
			continue
		}
		for _, tool := range decl.Tools {
			if tool.McpServer == nil {
				continue
			}
			if tool.McpServer.ApiGroup != "kagent.dev" || tool.McpServer.Kind != "MCPServer" {
				continue
			}
			if tool.McpServer.NamespacedName(sa.Namespace) == obj {
				out = append(out, sa)
			}
		}
	}
	return out
}

func (r *SandboxAgentController) findSandboxAgentsUsingRemoteMCPServer(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.SandboxAgent {
	var out []*v1alpha2.SandboxAgent

	var list v1alpha2.SandboxAgentList
	if err := cl.List(ctx, &list); err != nil {
		sandboxAgentControllerLog.Error(err, "failed to list sandboxagents for RemoteMCPServer watch")
		return out
	}

	for i := range list.Items {
		sa := &list.Items[i]
		decl := sa.Spec.Declarative
		if decl == nil {
			continue
		}
		for _, tool := range decl.Tools {
			if tool.McpServer == nil {
				continue
			}
			mcpServerRef := tool.McpServer.NamespacedName(sa.Namespace)
			if mcpServerRef == obj {
				out = append(out, sa)
				break
			}
		}
	}
	return out
}

func (r *SandboxAgentController) findSandboxAgentsUsingMCPService(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.SandboxAgent {
	var list v1alpha2.SandboxAgentList
	if err := cl.List(ctx, &list); err != nil {
		sandboxAgentControllerLog.Error(err, "failed to list sandboxagents for Service watch")
		return nil
	}

	var out []*v1alpha2.SandboxAgent
	for i := range list.Items {
		sa := &list.Items[i]
		decl := sa.Spec.Declarative
		if decl == nil {
			continue
		}
		for _, tool := range decl.Tools {
			if tool.McpServer == nil {
				continue
			}
			if tool.McpServer.ApiGroup != "" || tool.McpServer.Kind != "Service" {
				continue
			}
			if tool.McpServer.NamespacedName(sa.Namespace) == obj {
				out = append(out, sa)
			}
		}
	}
	return out
}

func (r *SandboxAgentController) findSandboxAgentsUsingModelConfig(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.SandboxAgent {
	var list v1alpha2.SandboxAgentList
	if err := cl.List(ctx, &list); err != nil {
		sandboxAgentControllerLog.Error(err, "failed to list sandboxagents for ModelConfig watch")
		return nil
	}

	var out []*v1alpha2.SandboxAgent
	for i := range list.Items {
		sa := &list.Items[i]
		if sa.Namespace != obj.Namespace {
			continue
		}
		if sa.Spec.Declarative != nil && sa.Spec.Declarative.ModelConfig == obj.Name {
			out = append(out, sa)
		}
	}
	return out
}

func (r *SandboxAgentController) findSandboxAgentsReferencingConfigMap(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.SandboxAgent {
	var list v1alpha2.SandboxAgentList
	if err := cl.List(ctx, &list); err != nil {
		sandboxAgentControllerLog.Error(err, "failed to list sandboxagents for ConfigMap watch")
		return nil
	}

	var out []*v1alpha2.SandboxAgent
	for i := range list.Items {
		sa := &list.Items[i]
		if sa.Namespace != obj.Namespace {
			continue
		}
		decl := sa.Spec.Declarative
		if decl == nil {
			continue
		}

		if ref := decl.SystemMessageFrom; ref != nil {
			if ref.Type == v1alpha2.ConfigMapValueSource && ref.Name == obj.Name {
				out = append(out, sa)
				continue
			}
		}

		if pt := decl.PromptTemplate; pt != nil {
			for _, ds := range pt.DataSources {
				if ds.Name == obj.Name {
					out = append(out, sa)
					break
				}
			}
		}
	}

	return out
}
