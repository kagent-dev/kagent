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

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/toolcatalog"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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
	RefreshToolServer(context.Context, *database.ToolServer, ...*v1alpha3.MCPTool) error
	DeleteToolServer(context.Context, string, string) error
}

// Reconciler keeps the catalog projection of KMCP-owned MCPServers current. It
// deliberately does not write MCPServer status, which is owned by KMCP.
type Reconciler struct {
	client     client.Client
	discoverer ToolDiscoverer
	catalog    CatalogStore
}

func New(client client.Client, discoverer ToolDiscoverer, catalog CatalogStore) *Reconciler {
	return &Reconciler{client: client, discoverer: discoverer, catalog: catalog}
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	installed, err := controllerEnabled(manager.GetRESTMapper())
	if err != nil {
		return err
	}
	if !installed {
		logging.FromLogr(manager.GetLogger()).InfoContext(context.Background(), "catalog discovery disabled because MCPServer CRD was not found")
		return nil
	}
	// KMCP reports deployment readiness through status without changing the
	// MCPServer generation, so a readiness flip is watched next to generation
	// and label changes; every other status write (replicas, observed
	// generation, timestamps) is ignored.
	return ctrl.NewControllerManagedBy(manager).
		WithOptions(controller.Options{NeedLeaderElection: new(true)}).
		For(&kmcp.MCPServer{}, builder.WithPredicates(predicate.Or[client.Object](
			predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, readinessChangedPredicate{},
		))).
		Named("mcpserver-catalog").
		Complete(r)
}

// readinessChangedPredicate passes an update that flips the MCPServer's Ready
// condition, the one status change the catalog depends on.
type readinessChangedPredicate struct {
	predicate.Funcs
}

func (readinessChangedPredicate) Update(e event.UpdateEvent) bool {
	before, ok := e.ObjectOld.(*kmcp.MCPServer)
	if !ok {
		return false
	}
	after, ok := e.ObjectNew.(*kmcp.MCPServer)
	if !ok {
		return false
	}
	return isReady(before) != isReady(after)
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
		return reconcile.Result{}, r.catalog.DeleteToolServer(ctx, request.String(), mcpServerGroupKind)
	}

	if discoveryDisabled(server) {
		// The operator opted this server out of discovery (for example because
		// agentgateway fronts it and agents reach it through a RemoteMCPServer).
		// Do not connect; keep the server in the catalog, disconnected, with no
		// tools, the same projection a RemoteMCPServer gets. Adding or removing
		// the label re-enters Reconcile at once; no requeue is needed.
		if err := r.updateCatalog(ctx, server, nil, false); err != nil {
			return reconcile.Result{}, fmt.Errorf("clear opted-out MCPServer catalog: %w", err)
		}
		return reconcile.Result{}, nil
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
		catalogErr := r.updateCatalog(ctx, server, nil, false)
		return reconcile.Result{}, errors.Join(
			fmt.Errorf("discover MCPServer tools: %w", err),
			wrapError("clear MCPServer tool catalog", catalogErr),
		)
	}

	discovered, err := toolcatalog.NormalizeTools(tools)
	if err != nil {
		catalogErr := r.updateCatalog(ctx, server, nil, false)
		return reconcile.Result{}, errors.Join(
			err,
			wrapError("clear invalid MCPServer tool catalog", catalogErr),
		)
	}
	if err := r.updateCatalog(ctx, server, discovered, true); err != nil {
		return reconcile.Result{}, fmt.Errorf("update MCPServer tool catalog: %w", err)
	}
	return reconcile.Result{RequeueAfter: refreshInterval}, nil
}

func isReady(server *kmcp.MCPServer) bool {
	condition := apiMeta.FindStatusCondition(server.Status.Conditions, string(kmcp.MCPServerConditionReady))
	return condition != nil && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == server.Generation
}

// discoveryDisabled reports whether the operator opted the server out of tool
// discovery with the kagent.dev/discovery=disabled label.
func discoveryDisabled(server *kmcp.MCPServer) bool {
	return server.Labels[consts.DiscoveryLabel] == consts.DiscoveryDisabled
}

func (r *Reconciler) updateCatalog(ctx context.Context, server *kmcp.MCPServer, tools []*v1alpha3.MCPTool, connected bool) error {
	name := client.ObjectKeyFromObject(server).String()
	var lastConnected *time.Time
	if connected {
		now := time.Now().UTC()
		lastConnected = &now
	}
	return r.catalog.RefreshToolServer(ctx, &database.ToolServer{
		Name: name, GroupKind: mcpServerGroupKind, Description: "N/A", LastConnected: lastConnected,
	}, tools...)
}

func wrapError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

var _ reconcile.Reconciler = (*Reconciler)(nil)
