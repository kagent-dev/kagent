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
	modelProviderConfigControllerLog = ctrl.Log.WithName("modelproviderconfig-controller")
)

// ModelProviderConfigController reconciles a ModelProviderConfig object
type ModelProviderConfigController struct {
	Scheme     *runtime.Scheme
	Reconciler reconciler.KagentReconciler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=modelproviderconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=modelproviderconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=modelproviderconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *ModelProviderConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	return r.Reconciler.ReconcileKagentModelProviderConfig(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelProviderConfigController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha2.ModelProviderConfig{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				for _, mpc := range r.findModelProviderConfigsUsingSecret(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      mpc.ObjectMeta.Name,
							Namespace: mpc.ObjectMeta.Namespace,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("modelproviderconfig").
		Complete(r)
}

func (r *ModelProviderConfigController) findModelProviderConfigsUsingSecret(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.ModelProviderConfig {
	var configs []*v1alpha2.ModelProviderConfig

	var configList v1alpha2.ModelProviderConfigList
	if err := cl.List(
		ctx,
		&configList,
	); err != nil {
		modelProviderConfigControllerLog.Error(err, "failed to list ModelProviderConfigs in order to reconcile Secret update")
		return configs
	}

	for i := range configList.Items {
		mpc := &configList.Items[i]

		if modelProviderConfigReferencesSecret(mpc, obj) {
			configs = append(configs, mpc)
		}
	}

	return configs
}

func modelProviderConfigReferencesSecret(mpc *v1alpha2.ModelProviderConfig, secretObj types.NamespacedName) bool {
	// Secrets must be in the same namespace as the model provider config
	return mpc.Namespace == secretObj.Namespace &&
		mpc.Spec.SecretRef.Name == secretObj.Name
}
