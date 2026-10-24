/*
Copyright 2026.

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

// Package remotemcpserver reconciles the discovered tool catalog published in
// RemoteMCPServer status.
package remotemcpserver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/toolcatalog"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

const (
	eventReconcileFailed  = "ReconcileFailed"
	eventValidationFailed = "ValidationFailed"
	eventToolsDiscovered  = "ToolsDiscovered"
	actionReconcile       = "Reconcile"
	actionDiscover        = "Discover"
	conditionAccepted     = "Accepted"
	refreshInterval       = 5 * time.Minute

	// discoveryDisabledMessage explains an Accepted RemoteMCPServer that publishes
	// no discovered tools because its operator opted out of discovery.
	discoveryDisabledMessage = "Tool discovery is disabled by the " + consts.DiscoveryLabel + "=" + consts.DiscoveryDisabled +
		" label; agents resolve the tool list at run time"
)

var remoteGroupKind = v1alpha3.GroupVersion.WithKind("RemoteMCPServer").GroupKind().String()

// ToolDiscoverer returns the tools currently advertised by one MCP server.
type ToolDiscoverer interface {
	ListTools(context.Context, toolservice.MCPServerRef) ([]toolservice.MCPAppTool, error)
}

// Reconciler publishes RemoteMCPServer discovery results to its status.
type Reconciler struct {
	client     client.Client
	discoverer ToolDiscoverer
	catalog    *toolcatalog.Publisher
	recorder   events.EventRecorder
}

func New(client client.Client, discoverer ToolDiscoverer, catalog toolcatalog.Store, recorder events.EventRecorder) *Reconciler {
	return &Reconciler{client: client, discoverer: discoverer, catalog: toolcatalog.NewPublisher(catalog), recorder: recorder}
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		// A label change does not bump the generation, and the discovery opt-out
		// is a label: watch both so the opt-out (and its removal) applies at once
		// instead of on the next periodic refresh.
		For(&v1alpha3.RemoteMCPServer{}, builder.WithPredicates(predicate.Or[client.Object](
			predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{},
		))).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.requestsForDependency)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.requestsForDependency)).
		Complete(r)
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	server := &v1alpha3.RemoteMCPServer{}
	if err := r.client.Get(ctx, request.NamespacedName, server); err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, r.catalog.Delete(ctx, request.String(), remoteGroupKind)
	}

	if discoveryDisabled(server) {
		// The operator opted this server out of discovery (a server that
		// authenticates every caller has no credential to offer the controller).
		// Accept it without listing tools: the status carries none and the catalog
		// keeps the server, disconnected, with no tools.
		if err := r.updateStatus(ctx, server, nil, metav1.ConditionTrue, "DiscoveryDisabled", discoveryDisabledMessage); err != nil {
			return reconcile.Result{}, fmt.Errorf("update RemoteMCPServer discovery status: %w", err)
		}
		if err := r.updateCatalog(ctx, server, nil, false); err != nil {
			return reconcile.Result{}, fmt.Errorf("update RemoteMCPServer tool catalog: %w", err)
		}
		return reconcile.Result{RequeueAfter: refreshInterval}, nil
	}

	tools, err := r.discoverer.ListTools(ctx, toolservice.MCPServerRef{
		Ref: request.NamespacedName, GroupKind: remoteGroupKind,
	})
	if err != nil {
		r.recordEvent(server, corev1.EventTypeWarning, eventReconcileFailed, actionReconcile,
			"failed to discover RemoteMCPServer tools: %v", err)
		statusErr := r.updateStatus(ctx, server, nil, metav1.ConditionFalse, "DiscoveryFailed", err.Error())
		catalogErr := r.updateCatalog(ctx, server, nil, false)
		return reconcile.Result{}, errors.Join(
			fmt.Errorf("discover RemoteMCPServer tools: %w", err),
			wrapError("update RemoteMCPServer discovery failure", statusErr),
			wrapError("clear RemoteMCPServer tool catalog", catalogErr),
		)
	}

	discovered, err := toolcatalog.NormalizeTools(tools)
	if err != nil {
		r.recordEvent(server, corev1.EventTypeWarning, eventValidationFailed, actionReconcile,
			"invalid RemoteMCPServer tool discovery: %v", err)
		statusErr := r.updateStatus(ctx, server, nil, metav1.ConditionFalse, "InvalidDiscovery", err.Error())
		catalogErr := r.updateCatalog(ctx, server, nil, false)
		return reconcile.Result{}, errors.Join(
			err,
			wrapError("update invalid RemoteMCPServer discovery", statusErr),
			wrapError("clear invalid RemoteMCPServer tool catalog", catalogErr),
		)
	}
	// Only surface a Normal event on the transition to a successful discovery,
	// so the periodic refresh timer does not re-emit it forever.
	accepted := apiMeta.FindStatusCondition(server.Status.Conditions, conditionAccepted)
	firstDiscovery := accepted == nil || accepted.Status != metav1.ConditionTrue || accepted.ObservedGeneration != server.Generation
	message := fmt.Sprintf("Discovered %d MCP tools", len(discovered))
	if err := r.updateStatus(ctx, server, discovered, metav1.ConditionTrue, "DiscoverySucceeded", message); err != nil {
		r.recordEvent(server, corev1.EventTypeWarning, eventReconcileFailed, actionReconcile,
			"failed to update RemoteMCPServer discovery status: %v", err)
		return reconcile.Result{}, fmt.Errorf("update RemoteMCPServer discovery status: %w", err)
	}
	if err := r.updateCatalog(ctx, server, discovered, true); err != nil {
		r.recordEvent(server, corev1.EventTypeWarning, eventReconcileFailed, actionReconcile,
			"failed to update RemoteMCPServer tool catalog: %v", err)
		return reconcile.Result{}, fmt.Errorf("update RemoteMCPServer tool catalog: %w", err)
	}
	if firstDiscovery {
		r.recordEvent(server, corev1.EventTypeNormal, eventToolsDiscovered, actionDiscover, "Discovered %d MCP tools", len(discovered))
	}
	return reconcile.Result{RequeueAfter: refreshInterval}, nil
}

// recordEvent emits a Kubernetes Event against the reconciled object. It is a
// no-op when no recorder is wired on this reconciler.
func (r *Reconciler) recordEvent(object client.Object, eventType, reason, action, messageFmt string, args ...any) {
	if r.recorder == nil {
		return
	}
	r.recorder.Eventf(object, nil, eventType, reason, action, messageFmt, args...)
}

// discoveryDisabled reports whether the operator opted the server out of tool
// discovery with the kagent.dev/discovery=disabled label.
func discoveryDisabled(server *v1alpha3.RemoteMCPServer) bool {
	return server.Labels[consts.DiscoveryLabel] == consts.DiscoveryDisabled
}

func (r *Reconciler) updateCatalog(ctx context.Context, server *v1alpha3.RemoteMCPServer, tools []*v1alpha3.MCPTool, connected bool) error {
	name := client.ObjectKeyFromObject(server).String()
	var lastConnected *time.Time
	if connected {
		now := time.Now().UTC()
		lastConnected = &now
	}
	return r.catalog.Refresh(ctx, server.UID, &database.ToolServer{
		Name: name, GroupKind: remoteGroupKind, Description: server.Spec.Description, LastConnected: lastConnected,
	}, tools...)
}

func wrapError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

func (r *Reconciler) updateStatus(
	ctx context.Context,
	server *v1alpha3.RemoteMCPServer,
	tools []*v1alpha3.MCPTool,
	conditionStatus metav1.ConditionStatus,
	reason string,
	message string,
) error {
	original := server.DeepCopy()
	server.Status.ObservedGeneration = server.Generation
	server.Status.DiscoveredTools = tools
	apiMeta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type: conditionAccepted, Status: conditionStatus, Reason: reason, Message: message,
		ObservedGeneration: server.Generation,
	})
	if reflect.DeepEqual(original.Status, server.Status) {
		return nil
	}
	return r.client.Status().Patch(ctx, server, client.MergeFrom(original))
}

func (r *Reconciler) requestsForDependency(ctx context.Context, object client.Object) []reconcile.Request {
	servers := &v1alpha3.RemoteMCPServerList{}
	if err := r.client.List(ctx, servers, client.InNamespace(object.GetNamespace())); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for i := range servers.Items {
		server := &servers.Items[i]
		if referencesDependency(server, object) {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: server.Namespace, Name: server.Name,
			}})
		}
	}
	return requests
}

func referencesDependency(server *v1alpha3.RemoteMCPServer, object client.Object) bool {
	switch object.(type) {
	case *corev1.Secret:
		for i := range server.Spec.HeadersFrom {
			from := server.Spec.HeadersFrom[i].ValueFrom
			if from != nil && from.Type == v1alpha3.SecretValueSource && from.Name == object.GetName() {
				return true
			}
		}
	case *corev1.ConfigMap:
		for i := range server.Spec.HeadersFrom {
			from := server.Spec.HeadersFrom[i].ValueFrom
			if from != nil && from.Type == v1alpha3.ConfigMapValueSource && from.Name == object.GetName() {
				return true
			}
		}
	}
	return false
}

var _ reconcile.Reconciler = (*Reconciler)(nil)
