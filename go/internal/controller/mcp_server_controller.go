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

	"github.com/kagent-dev/kagent/go/internal/controller/reconciler"
	"github.com/kagent-dev/kmcp/api/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// MCPServerController reconciles a MCPServer object
type MCPServerController struct {
	Scheme     *runtime.Scheme
	Reconciler reconciler.KagentReconciler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=mcpservers,verbs=get;list;watch

func (r *MCPServerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	return ctrl.Result{
		// loop forever because we need to refresh tools server status
		RequeueAfter: 60 * time.Second,
	}, r.Reconciler.ReconcileKagentMCPServer(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha1.MCPServer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("toolserver").
		Complete(r)
}
