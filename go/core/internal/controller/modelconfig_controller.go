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

	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
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
	// Client is used to fetch the reconciled object so Events can be emitted
	// against it. Optional; event emission is skipped when nil.
	Client client.Client
	// Recorder emits Kubernetes Events on reconcile transitions. Optional.
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *ModelConfigController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	if err := r.Reconciler.ReconcileKagentModelConfig(ctx, req); err != nil {
		r.recordEvent(ctx, req, "Warning", "ReconcileFailed", "Reconcile",
			"failed to reconcile ModelConfig: %s", err.Error())
		return ctrl.Result{}, err
	}
	r.recordEvent(ctx, req, "Normal", "Accepted", "Reconcile", "ModelConfig reconciled successfully")
	return ctrl.Result{}, nil
}

// recordEvent emits a Kubernetes Event against the reconciled ModelConfig.
// No-op when no Recorder/Client is wired or the object cannot be fetched.
func (r *ModelConfigController) recordEvent(ctx context.Context, req ctrl.Request, eventtype, reason, action, note string, args ...any) {
	if r.Recorder == nil || r.Client == nil {
		return
	}
	mc := &v1alpha2.ModelConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, mc); err != nil {
		if !apierrors.IsNotFound(err) {
			log.FromContext(ctx).V(1).Info("unable to fetch ModelConfig for event recording", "error", err.Error())
		}
		return
	}
	r.Recorder.Eventf(mc, nil, eventtype, reason, action, note, args...)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelConfigController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha2.ModelConfig{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&corev1.Secret{},
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

		if modelReferencesSecret(model, obj) {
			models = append(models, model)
		}
	}

	return models
}

func modelReferencesSecret(model *v1alpha2.ModelConfig, secretObj types.NamespacedName) bool {
	// secrets must be in the same namespace as the model
	if model.Namespace != secretObj.Namespace {
		return false
	}

	// check if secret is referenced as an APIKey
	if model.Spec.APIKeySecret != "" && model.Spec.APIKeySecret == secretObj.Name {
		return true
	}

	// check if secret is referenced as a TLS CA certificate
	if model.Spec.TLS != nil && model.Spec.TLS.CACertSecretRef != "" && model.Spec.TLS.CACertSecretRef == secretObj.Name {
		return true
	}

	return false
}
