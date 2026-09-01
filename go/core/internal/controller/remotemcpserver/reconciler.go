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

	dbmodel "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/toolcatalog"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	conditionAccepted = "Accepted"
	remoteGroupKind   = "RemoteMCPServer.kagent.dev"
	refreshInterval   = 5 * time.Minute
)

// ToolDiscoverer returns the tools currently advertised by one MCP server.
type ToolDiscoverer interface {
	ListTools(context.Context, toolservice.MCPServerRef) ([]toolservice.MCPAppTool, error)
}

// CatalogStore keeps the gRPC ToolService catalog aligned with Kubernetes status.
// RemoteMCPServer status remains the source used by harness compilers, while the
// database projection serves list RPCs without making those RPCs perform discovery.
type CatalogStore interface {
	StoreToolServer(context.Context, *dbmodel.ToolServer) (*dbmodel.ToolServer, error)
	RefreshToolsForServer(context.Context, string, string, ...*v1alpha3.MCPTool) error
	DeleteToolsForServer(context.Context, string, string) error
	DeleteToolServer(context.Context, string, string) error
}

// Reconciler publishes RemoteMCPServer discovery results to its status.
type Reconciler struct {
	client     client.Client
	discoverer ToolDiscoverer
	catalog    CatalogStore
}

func New(client client.Client, discoverer ToolDiscoverer, catalog CatalogStore) *Reconciler {
	return &Reconciler{client: client, discoverer: discoverer, catalog: catalog}
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		For(&v1alpha3.RemoteMCPServer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
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
		return reconcile.Result{}, r.deleteCatalog(ctx, request.String())
	}

	tools, err := r.discoverer.ListTools(ctx, toolservice.MCPServerRef{
		Ref: request.NamespacedName, GroupKind: remoteGroupKind,
	})
	if err != nil {
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
		statusErr := r.updateStatus(ctx, server, nil, metav1.ConditionFalse, "InvalidDiscovery", err.Error())
		catalogErr := r.updateCatalog(ctx, server, nil, false)
		return reconcile.Result{}, errors.Join(
			err,
			wrapError("update invalid RemoteMCPServer discovery", statusErr),
			wrapError("clear invalid RemoteMCPServer tool catalog", catalogErr),
		)
	}
	message := fmt.Sprintf("Discovered %d MCP tools", len(discovered))
	if err := r.updateStatus(ctx, server, discovered, metav1.ConditionTrue, "DiscoverySucceeded", message); err != nil {
		return reconcile.Result{}, fmt.Errorf("update RemoteMCPServer discovery status: %w", err)
	}
	if err := r.updateCatalog(ctx, server, discovered, true); err != nil {
		return reconcile.Result{}, fmt.Errorf("update RemoteMCPServer tool catalog: %w", err)
	}
	return reconcile.Result{RequeueAfter: refreshInterval}, nil
}

func (r *Reconciler) updateCatalog(ctx context.Context, server *v1alpha3.RemoteMCPServer, tools []*v1alpha3.MCPTool, connected bool) error {
	name := client.ObjectKeyFromObject(server).String()
	var lastConnected *time.Time
	if connected {
		now := time.Now().UTC()
		lastConnected = &now
	}
	if _, err := r.catalog.StoreToolServer(ctx, &dbmodel.ToolServer{
		Name: name, GroupKind: remoteGroupKind, Description: server.Spec.Description, LastConnected: lastConnected,
	}); err != nil {
		return fmt.Errorf("store server: %w", err)
	}
	if err := r.catalog.RefreshToolsForServer(ctx, name, remoteGroupKind, tools...); err != nil {
		return fmt.Errorf("refresh tools: %w", err)
	}
	return nil
}

func (r *Reconciler) deleteCatalog(ctx context.Context, name string) error {
	return errors.Join(
		wrapError("delete tools", r.catalog.DeleteToolsForServer(ctx, name, remoteGroupKind)),
		wrapError("delete server", r.catalog.DeleteToolServer(ctx, name, remoteGroupKind)),
	)
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
		if server.Spec.TLS != nil && server.Spec.TLS.CACertSecretRef == object.GetName() {
			return true
		}
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
