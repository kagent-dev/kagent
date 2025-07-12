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

	"github.com/kagent-dev/kagent/go/controller/internal/autogen"
	v1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// AutogenModelConfigReconciler reconciles a Secret object which contains a model config
type AutogenSecretReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Reconciler autogen.AutogenReconciler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=modelconfigs/finalizers,verbs=update

func (r *AutogenSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	return ctrl.Result{}, r.Reconciler.ReconcileAutogenApiKeySecret(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutogenSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1.Secret{}).
		Named("autogenapikeysecret").
		Complete(r)
}
