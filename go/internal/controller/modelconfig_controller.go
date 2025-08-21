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

	"github.com/kagent-dev/kagent/go/internal/controller/reconciler"

	v1 "k8s.io/api/core/v1"
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

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

var (
	modelConfigControllerLog = ctrl.Log.WithName("modelconfig-controller")
)

// ModelConfigController reconciles a ModelConfig object
type ModelConfigController struct {
	Scheme     *runtime.Scheme
	Reconciler reconciler.KagentReconciler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs/finalizers,verbs=update

func (r *ModelConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	return ctrl.Result{}, r.Reconciler.ReconcileKagentModelConfig(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelConfigController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha2.ModelConfig{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				requests := []reconcile.Request{}

				for _, model := range r.findModelsUsingSecret(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      model.ObjectMeta.Name,
							Namespace: model.ObjectMeta.Namespace,
						},
					})
				}

				return requests
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("modelconfig").
		Complete(r)
}

func (r *ModelConfigController) findModelsUsingSecret(ctx context.Context, cl client.Client, obj types.NamespacedName) []*v1alpha2.ModelConfig {
	var models []*v1alpha2.ModelConfig

	var modelsList v1alpha2.ModelConfigList
	if err := cl.List(
		ctx,
		&modelsList,
	); err != nil {
		modelConfigControllerLog.Error(err, "failed to list ModelConfigs in order to reconcile Secret update")
		return models
	}

	for i := range modelsList.Items {
		model := &modelsList.Items[i]

		if model.Namespace != obj.Namespace {
			continue
		}

		if model.Spec.APIKeySecret == "" {
			continue
		}
		if model.Spec.APIKeySecret == obj.Name {
			models = append(models, model)
		}
	}

	return models
}
