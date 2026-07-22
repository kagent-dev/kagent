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

	"github.com/kagent-dev/kagent/go/core/internal/controller/predicates"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"

	"github.com/kagent-dev/kmcp/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var mcpServerGK = schema.GroupKind{Group: "kagent.dev", Kind: "MCPServer"}

// MCPServerToolController handles reconciliation of a MCPServer object for tool discovery purposes
type MCPServerToolController struct {
	Scheme     *runtime.Scheme
	Reconciler reconciler.KagentReconciler
	// Client is used to fetch the reconciled object so Events can be emitted
	// against it. Optional; event emission is skipped when nil.
	Client client.Client
	// Recorder emits Kubernetes Events on reconcile transitions. Optional.
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=kagent.dev,resources=mcpservers,verbs=get;list;watch

func (r *MCPServerToolController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// This controller requeues every 60s to refresh tool status. Only emit the
	// success Event on a meaningful transition (a spec change not yet observed
	// in status), not on every periodic refresh, to avoid flooding Events.
	specChanged := r.hasPendingGeneration(ctx, req)

	err := r.Reconciler.ReconcileKagentMCPServer(ctx, req)
	if err != nil {
		// Check if this is a validation error that requires user action
		var validationErr *agent_translator.ValidationError
		if errors.As(err, &validationErr) {
			r.recordEvent(ctx, req, "Warning", "ValidationFailed", "Reconcile",
				"MCPServer validation failed: %s", err.Error())
			// Validation error - don't retry until MCPServer spec is updated
			// Return empty result with no error to avoid exponential backoff
			return ctrl.Result{}, nil
		}
		r.recordEvent(ctx, req, "Warning", "ReconcileFailed", "Reconcile",
			"failed to reconcile MCPServer: %s", err.Error())
		// Transient error - return error to trigger exponential backoff retry
		return ctrl.Result{}, err
	}
	if specChanged {
		r.recordEvent(ctx, req, "Normal", "ToolsDiscovered", "Reconcile", "MCPServer tools discovered successfully")
	}
	// Success - requeue after 60s to refresh tool server status
	return ctrl.Result{
		RequeueAfter: 60 * time.Second,
	}, nil
}

// hasPendingGeneration reports whether the MCPServer has spec changes not yet
// reflected in status (generation != observedGeneration). It gates success
// Events to meaningful transitions. Returns true (emit) when the object can't
// be fetched, so events are not silently dropped.
func (r *MCPServerToolController) hasPendingGeneration(ctx context.Context, req ctrl.Request) bool {
	if r.Client == nil {
		return true
	}
	server := &v1alpha1.MCPServer{}
	if err := r.Client.Get(ctx, req.NamespacedName, server); err != nil {
		return true
	}
	return server.Generation != server.Status.ObservedGeneration
}

// recordEvent emits a Kubernetes Event against the reconciled MCPServer.
// No-op when no Recorder/Client is wired or the object cannot be fetched.
func (r *MCPServerToolController) recordEvent(ctx context.Context, req ctrl.Request, eventtype, reason, action, note string, args ...any) {
	if r.Recorder == nil || r.Client == nil {
		return
	}
	server := &v1alpha1.MCPServer{}
	if err := r.Client.Get(ctx, req.NamespacedName, server); err != nil {
		if !apierrors.IsNotFound(err) {
			log.FromContext(ctx).V(1).Info("unable to fetch MCPServer for event recording", "error", err.Error())
		}
		return
	}
	r.Recorder.Eventf(server, nil, eventtype, reason, action, note, args...)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerToolController) SetupWithManager(mgr ctrl.Manager) error {
	if _, err := mgr.GetRESTMapper().RESTMapping(mcpServerGK); err != nil {
		ctrl.Log.Info("MCPServer CRD not found - controller will not be started", "controller", "mcpserver")
		return nil
	}
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			NeedLeaderElection: new(true),
		}).
		For(&v1alpha1.MCPServer{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
			predicates.DiscoveryDisabledPredicate{},
		)).
		Named("toolserver").
		Complete(r)
}
