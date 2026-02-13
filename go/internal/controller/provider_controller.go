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

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/reconciler"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
	providerControllerLog = ctrl.Log.WithName("provider-controller")
)

// ProviderController reconciles a Provider object
type ProviderController struct {
	Scheme     *runtime.Scheme
	Reconciler reconciler.KagentReconciler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=providers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=providers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=providers/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *ProviderController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	return r.Reconciler.ReconcileKagentProvider(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha2.Provider{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				for _, provider := range r.findProvidersUsingSecret(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      provider.ObjectMeta.Name,
							Namespace: provider.ObjectMeta.Namespace,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("provider").
		Complete(r)
}

func (r *ProviderController) findProvidersUsingSecret(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.Provider {
	var providers []*v1alpha2.Provider

	var providersList v1alpha2.ProviderList
	if err := cl.List(
		ctx,
		&providersList,
	); err != nil {
		providerControllerLog.Error(err, "failed to list Providers in order to reconcile Secret update")
		return providers
	}

	for i := range providersList.Items {
		provider := &providersList.Items[i]

		if providerReferencesSecret(provider, obj) {
			providers = append(providers, provider)
		}
	}

	return providers
}

func providerReferencesSecret(provider *v1alpha2.Provider, secretObj types.NamespacedName) bool {
	// Secrets must be in the same namespace as the provider
	return provider.Namespace == secretObj.Namespace &&
		provider.Spec.SecretRef.Name == secretObj.Name
}
