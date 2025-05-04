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

	"github.com/kagent-dev/kagent/go/controller/internal/autogen"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	agentv1alpha1 "github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
)

// ToolServerReconciler reconciles a ToolServer object
type ToolServerReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Reconciler autogen.AutogenReconciler
}

// +kubebuilder:rbac:groups=agent.kagent.dev,resources=toolservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.kagent.dev,resources=toolservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent.kagent.dev,resources=toolservers/finalizers,verbs=update

func (r *ToolServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	return ctrl.Result{
		// loop forever because we need to refresh tools server status
		Requeue:      true,
		RequeueAfter: 60 * time.Second,
	}, r.Reconciler.ReconcileAutogenToolServer(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ToolServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.ToolServer{}).
		Named("toolserver").
		Complete(r)
}
