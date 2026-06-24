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
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	remoteMCPServerControllerLog = ctrl.Log.WithName("remotemcpserver-controller")
)

// RemoteMCPServerController reconciles a RemoteMCPServer object
type RemoteMCPServerController struct {
	Scheme     *runtime.Scheme
	Reconciler reconciler.KagentReconciler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=remotemcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=remotemcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=remotemcpservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *RemoteMCPServerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	err := r.Reconciler.ReconcileKagentRemoteMCPServer(ctx, req)
	if err != nil {
		// Return zero result when there's an error - controller-runtime will handle backoff
		return ctrl.Result{}, err
	}
	// Success - requeue after 60s to refresh tool server status
	return ctrl.Result{
		RequeueAfter: 60 * time.Second,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteMCPServerController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.RemoteMCPServer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				for _, server := range r.findRemoteMCPServersUsingSecret(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      server.ObjectMeta.Name,
							Namespace: server.ObjectMeta.Namespace,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("remotemcpserver").
		Complete(r)
}

func (r *RemoteMCPServerController) findRemoteMCPServersUsingSecret(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.RemoteMCPServer {
	var servers []*v1alpha2.RemoteMCPServer

	var serverList v1alpha2.RemoteMCPServerList
	if err := cl.List(
		ctx,
		&serverList,
	); err != nil {
		remoteMCPServerControllerLog.Error(err, "failed to list RemoteMCPServers in order to reconcile Secret update")
		return servers
	}

	for i := range serverList.Items {
		server := &serverList.Items[i]

		if remoteMCPServerReferencesSecret(server, obj) {
			servers = append(servers, server)
		}
	}

	return servers
}

func remoteMCPServerReferencesSecret(server *v1alpha2.RemoteMCPServer, secretObj types.NamespacedName) bool {
	// secrets must be in the same namespace as the RemoteMCPServer
	if server.Namespace != secretObj.Namespace {
		return false
	}

	// check if secret is referenced as a TLS CA certificate
	if server.Spec.TLS != nil && server.Spec.TLS.CACertSecretRef != "" && server.Spec.TLS.CACertSecretRef == secretObj.Name {
		return true
	}

	return false
}
