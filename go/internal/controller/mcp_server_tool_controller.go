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
	"errors"
	"time"

	"github.com/kagent-dev/kagent/go/internal/controller/predicates"
	"github.com/kagent-dev/kagent/go/internal/controller/reconciler"
	agent_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"

	"github.com/kagent-dev/kmcp/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var mcpServerGK = schema.GroupKind{Group: "kagent.dev", Kind: "MCPServer"}

// MCPServerToolController handles reconciliation of a MCPServer object for tool discovery purposes
type MCPServerToolController struct {
	Scheme     *runtime.Scheme
	Reconciler reconciler.KagentReconciler
}

// +kubebuilder:rbac:groups=kagent.dev,resources=mcpservers,verbs=get;list;watch

func (r *MCPServerToolController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	err := r.Reconciler.ReconcileKagentMCPServer(ctx, req)
	if err != nil {
		// Check if this is a validation error that requires user action
		var validationErr *agent_translator.ValidationError
		if errors.As(err, &validationErr) {
			// Validation error - don't retry until MCPServer spec is updated
			// Return empty result with no error to avoid exponential backoff
			return ctrl.Result{}, nil
		}
		// Transient error - return error to trigger exponential backoff retry
		return ctrl.Result{}, err
	}
	// Success - requeue after 60s to refresh tool server status
	return ctrl.Result{
		RequeueAfter: 60 * time.Second,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerToolController) SetupWithManager(mgr ctrl.Manager) error {
	if _, err := mgr.GetRESTMapper().RESTMapping(mcpServerGK); err != nil {
		ctrl.Log.Info("MCPServer CRD not found - controller will not be started", "controller", "mcpserver")
		return nil
	}
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(true),
		}).
		For(&v1alpha1.MCPServer{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
			predicates.DiscoveryDisabledPredicate{},
		)).
		Named("toolserver").
		Complete(r)
}
