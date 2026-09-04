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

// Package mcpserver projects tools discovered from KMCP MCPServers into the
// catalog served by kagent's ToolService.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbmodel "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/toolcatalog"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	mcpServerGroupKind    = "MCPServer.kagent.dev"
	refreshInterval       = 5 * time.Minute
	readinessPollInterval = 10 * time.Second
)

var mcpServerGK = schema.GroupKind{Group: kmcp.GroupVersion.Group, Kind: "MCPServer"}

// ToolDiscoverer returns the tools currently advertised by one MCP server.
type ToolDiscoverer interface {
	ListTools(context.Context, toolservice.MCPServerRef) ([]toolservice.MCPAppTool, error)
}

// CatalogStore persists the ToolService projection of an MCPServer.
type CatalogStore interface {
	StoreToolServer(context.Context, *dbmodel.ToolServer) (*dbmodel.ToolServer, error)
	RefreshToolsForServer(context.Context, string, string, ...*v1alpha3.MCPTool) error
	DeleteToolsForServer(context.Context, string, string) error
	DeleteToolServer(context.Context, string, string) error
}

// Reconciler keeps the catalog projection of KMCP-owned MCPServers current. It
// deliberately does not write MCPServer status, which is owned by KMCP.
type Reconciler struct {
	client     client.Client
	discoverer ToolDiscoverer
	catalog    CatalogStore
	// Recorder emits Kubernetes Events on reconcile transitions. Optional;
	// event emission is skipped when nil.
	Recorder events.EventRecorder
}

func New(client client.Client, discoverer ToolDiscoverer, catalog CatalogStore) *Reconciler {
	return &Reconciler{client: client, discoverer: discoverer, catalog: catalog}
}

// WithRecorder wires an optional EventRecorder used to surface reconcile
// outcomes via kubectl describe. It returns the receiver so it composes with
// New.
func (r *Reconciler) WithRecorder(recorder events.EventRecorder) *Reconciler {
	r.Recorder = recorder
	return r
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	installed, err := controllerEnabled(manager.GetRESTMapper())
	if err != nil {
		return err
	}
	if !installed {
		ctrl.Log.Info("MCPServer CRD not found; catalog discovery will not be started")
		return nil
	}
	// Status changes are intentionally observed: KMCP reports deployment
	// readiness through status without changing the MCPServer generation.
	return ctrl.NewControllerManagedBy(manager).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&kmcp.MCPServer{}).
		Named("mcpserver-catalog").
		Complete(r)
}

func controllerEnabled(mapper apiMeta.RESTMapper) (bool, error) {
	if _, err := mapper.RESTMapping(mcpServerGK); err != nil {
		if apiMeta.IsNoMatchError(err) {
			return false, nil
		}
		return false, fmt.Errorf("resolve MCPServer REST mapping: %w", err)
	}
	return true, nil
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	server := &kmcp.MCPServer{}
	if err := r.client.Get(ctx, request.NamespacedName, server); err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("get MCPServer %s: %w", request.String(), err)
		}
		return reconcile.Result{}, r.deleteCatalog(ctx, request.String())
	}

	if !isReady(server) {
		if err := r.updateCatalog(ctx, server, nil, false); err != nil {
			return reconcile.Result{}, fmt.Errorf("clear unready MCPServer catalog: %w", err)
		}
		return reconcile.Result{RequeueAfter: readinessPollInterval}, nil
	}

	tools, err := r.discoverer.ListTools(ctx, toolservice.MCPServerRef{
		Ref: request.NamespacedName, GroupKind: mcpServerGroupKind,
	})
	if err != nil {
		r.recordEvent(ctx, server, "Warning", "ReconcileFailed", "Reconcile",
			"failed to discover MCPServer tools: %v", err)
		catalogErr := r.updateCatalog(ctx, server, nil, false)
		return reconcile.Result{}, errors.Join(
			fmt.Errorf("discover MCPServer tools: %w", err),
			wrapError("clear MCPServer tool catalog", catalogErr),
		)
	}

	discovered, err := toolcatalog.NormalizeTools(tools)
	if err != nil {
		r.recordEvent(ctx, server, "Warning", "ValidationFailed", "Reconcile",
			"invalid MCPServer tool discovery: %v", err)
		catalogErr := r.updateCatalog(ctx, server, nil, false)
		return reconcile.Result{}, errors.Join(
			err,
			wrapError("clear invalid MCPServer tool catalog", catalogErr),
		)
	}
	if err := r.updateCatalog(ctx, server, discovered, true); err != nil {
		r.recordEvent(ctx, server, "Warning", "ReconcileFailed", "Reconcile",
			"failed to update MCPServer tool catalog: %v", err)
		return reconcile.Result{}, fmt.Errorf("update MCPServer tool catalog: %w", err)
	}
	if len(discovered) > 0 {
		r.recordEvent(ctx, server, "Normal", "ToolsDiscovered", "Discover", "Discovered %d MCP tools", len(discovered))
	}
	return reconcile.Result{RequeueAfter: refreshInterval}, nil
}

// recordEvent emits a Kubernetes Event against the reconciled object. It is a
// no-op when no Recorder is wired on this reconciler.
func (r *Reconciler) recordEvent(ctx context.Context, object client.Object, eventType, reason, action, messageFmt string, args ...any) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(object, nil, eventType, reason, action, messageFmt, args...)
}

func isReady(server *kmcp.MCPServer) bool {
	condition := apiMeta.FindStatusCondition(server.Status.Conditions, string(kmcp.MCPServerConditionReady))
	return condition != nil && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == server.Generation
}

func (r *Reconciler) updateCatalog(ctx context.Context, server *kmcp.MCPServer, tools []*v1alpha3.MCPTool, connected bool) error {
	name := client.ObjectKeyFromObject(server).String()
	var lastConnected *time.Time
	if connected {
		now := time.Now().UTC()
		lastConnected = &now
	}
	if _, err := r.catalog.StoreToolServer(ctx, &dbmodel.ToolServer{
		Name: name, GroupKind: mcpServerGroupKind, Description: "N/A", LastConnected: lastConnected,
	}); err != nil {
		return fmt.Errorf("store server: %w", err)
	}
	if err := r.catalog.RefreshToolsForServer(ctx, name, mcpServerGroupKind, tools...); err != nil {
		return fmt.Errorf("refresh tools: %w", err)
	}
	return nil
}

func (r *Reconciler) deleteCatalog(ctx context.Context, name string) error {
	return errors.Join(
		wrapError("delete tools", r.catalog.DeleteToolsForServer(ctx, name, mcpServerGroupKind)),
		wrapError("delete server", r.catalog.DeleteToolServer(ctx, name, mcpServerGroupKind)),
	)
}

func wrapError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

var _ reconcile.Reconciler = (*Reconciler)(nil)
