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

package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dbmodel "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeDiscoverer struct {
	tools []toolservice.MCPAppTool
	err   error
	ref   toolservice.MCPServerRef
	calls int
}

func (f *fakeDiscoverer) ListTools(_ context.Context, ref toolservice.MCPServerRef) ([]toolservice.MCPAppTool, error) {
	f.calls++
	f.ref = ref
	return f.tools, f.err
}

type fakeCatalog struct {
	server       *dbmodel.ToolServer
	tools        []*v1alpha3.MCPTool
	deletedTools string
	deleted      string
}

func (f *fakeCatalog) StoreToolServer(_ context.Context, server *dbmodel.ToolServer) (*dbmodel.ToolServer, error) {
	f.server = server
	return server, nil
}

func (f *fakeCatalog) RefreshToolsForServer(_ context.Context, _, _ string, tools ...*v1alpha3.MCPTool) error {
	f.tools = tools
	return nil
}

func (f *fakeCatalog) DeleteToolsForServer(_ context.Context, name, groupKind string) error {
	f.deletedTools = name + "|" + groupKind
	return nil
}

func (f *fakeCatalog) DeleteToolServer(_ context.Context, name, groupKind string) error {
	f.deleted = name + "|" + groupKind
	return nil
}

func TestReconcileDiscoversReadyMCPServer(t *testing.T) {
	server := readyServer()
	discoverer := &fakeDiscoverer{tools: []toolservice.MCPAppTool{
		{Name: "zeta", Description: "last"},
		{Name: "alpha", Description: "first"},
	}}
	catalog := &fakeCatalog{}

	result, err := New(testClient(t, server), discoverer, catalog).Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(server),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != 5*time.Minute {
		t.Fatalf("Reconcile() requeue = %s, want 5m", result.RequeueAfter)
	}
	if discoverer.ref.Ref != client.ObjectKeyFromObject(server) || discoverer.ref.GroupKind != mcpServerGroupKind {
		t.Fatalf("discovery ref = %#v", discoverer.ref)
	}
	if catalog.server == nil || catalog.server.Name != "test/tools" || catalog.server.GroupKind != mcpServerGroupKind || catalog.server.LastConnected == nil {
		t.Fatalf("catalog server = %#v", catalog.server)
	}
	if len(catalog.tools) != 2 || catalog.tools[0].Name != "alpha" || catalog.tools[1].Name != "zeta" {
		t.Fatalf("catalog tools = %#v", catalog.tools)
	}
}

func TestReconcileWaitsForCurrentReadyCondition(t *testing.T) {
	tests := []struct {
		name      string
		condition *metav1.Condition
	}{
		{name: "condition absent"},
		{name: "not ready", condition: &metav1.Condition{Type: string(kmcp.MCPServerConditionReady), Status: metav1.ConditionFalse, ObservedGeneration: 3}},
		{name: "stale ready", condition: &metav1.Condition{Type: string(kmcp.MCPServerConditionReady), Status: metav1.ConditionTrue, ObservedGeneration: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := testServer()
			if test.condition != nil {
				server.Status.Conditions = []metav1.Condition{*test.condition}
			}
			discoverer := &fakeDiscoverer{}
			catalog := &fakeCatalog{tools: []*v1alpha3.MCPTool{{Name: "stale"}}}

			result, err := New(testClient(t, server), discoverer, catalog).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(server),
			})
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if result.RequeueAfter != readinessPollInterval || discoverer.calls != 0 {
				t.Fatalf("Reconcile() = %#v, discovery calls = %d", result, discoverer.calls)
			}
			if catalog.server == nil || catalog.server.LastConnected != nil || len(catalog.tools) != 0 {
				t.Fatalf("unready catalog = server %#v, tools %#v", catalog.server, catalog.tools)
			}
		})
	}
}

func TestReconcileClearsCatalogAfterDiscoveryFailure(t *testing.T) {
	server := readyServer()
	discoverer := &fakeDiscoverer{err: errors.New("unavailable")}
	catalog := &fakeCatalog{tools: []*v1alpha3.MCPTool{{Name: "stale"}}}

	_, err := New(testClient(t, server), discoverer, catalog).Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(server),
	})
	if err == nil {
		t.Fatal("Reconcile() error = nil")
	}
	if catalog.server == nil || catalog.server.LastConnected != nil || len(catalog.tools) != 0 {
		t.Fatalf("failed discovery catalog = server %#v, tools %#v", catalog.server, catalog.tools)
	}
}

func TestReconcileDeletesCatalogProjection(t *testing.T) {
	catalog := &fakeCatalog{}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "gone"}}

	if _, err := New(testClient(t), &fakeDiscoverer{}, catalog).Reconcile(t.Context(), request); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	want := "test/gone|" + mcpServerGroupKind
	if catalog.deletedTools != want || catalog.deleted != want {
		t.Fatalf("catalog deletes = tools %q, server %q, want %q", catalog.deletedTools, catalog.deleted, want)
	}
}

func TestControllerEnabled(t *testing.T) {
	mapper := apiMetaTestMapper()
	installed, err := controllerEnabled(mapper)
	if err != nil || installed {
		t.Fatalf("controllerEnabled() = %t, %v; want false, nil", installed, err)
	}
	mapper.AddSpecific(
		kmcp.GroupVersion.WithKind("MCPServer"),
		kmcp.GroupVersion.WithResource("mcpservers"),
		kmcp.GroupVersion.WithResource("mcpserver"),
		apiMeta.RESTScopeNamespace,
	)
	installed, err = controllerEnabled(mapper)
	if err != nil || !installed {
		t.Fatalf("controllerEnabled() = %t, %v; want true, nil", installed, err)
	}
}

func testServer() *kmcp.MCPServer {
	return &kmcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "tools", Generation: 3}}
}

func readyServer() *kmcp.MCPServer {
	server := testServer()
	server.Status.Conditions = []metav1.Condition{{
		Type: string(kmcp.MCPServerConditionReady), Status: metav1.ConditionTrue, ObservedGeneration: server.Generation,
	}}
	return server
}

func testClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := kmcp.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func apiMetaTestMapper() *apiMeta.DefaultRESTMapper {
	return apiMeta.NewDefaultRESTMapper([]schema.GroupVersion{kmcp.GroupVersion})
}

// TestReconcileEmitsToolsDiscovered verifies a Normal ToolsDiscovered event is
// emitted when a ready MCPServer is successfully discovered.
func TestReconcileEmitsToolsDiscovered(t *testing.T) {
	server := readyServer()
	discoverer := &fakeDiscoverer{tools: []toolservice.MCPAppTool{{Name: "zeta"}}}
	recorder := events.NewFakeRecorder(1)

	result, err := New(testClient(t, server), discoverer, &fakeCatalog{}).
		WithRecorder(recorder).Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(server),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != 5*time.Minute {
		t.Fatalf("Reconcile() requeue = %s, want 5m", result.RequeueAfter)
	}

	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, "Normal ToolsDiscovered ") {
			t.Fatalf("unexpected event: %q", ev)
		}
	default:
		t.Fatal("expected a ToolsDiscovered event, got none")
	}
}

// TestReconcileEmitsValidationFailed verifies a malformed discovery triggers a
// Warning ValidationFailed event.
func TestReconcileEmitsValidationFailed(t *testing.T) {
	server := readyServer()
	discoverer := &fakeDiscoverer{tools: []toolservice.MCPAppTool{{Name: " "}}}
	recorder := events.NewFakeRecorder(1)

	if _, err := New(testClient(t, server), discoverer, &fakeCatalog{}).
		WithRecorder(recorder).Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(server),
	}); err == nil {
		t.Fatal("expected validation error")
	}

	ev := <-recorder.Events
	if !strings.Contains(ev, "Warning ValidationFailed ") {
		t.Fatalf("unexpected event: %q", ev)
	}
}

// TestReconcileNoEventsWithoutRecorder verifies event emission is skipped when
// no recorder is wired, keeping zero behavioral change by default.
func TestReconcileNoEventsWithoutRecorder(t *testing.T) {
	server := readyServer()
	discoverer := &fakeDiscoverer{tools: []toolservice.MCPAppTool{{Name: "zeta"}}}

	if _, err := New(testClient(t, server), discoverer, &fakeCatalog{}).
		Reconcile(t.Context(), ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(server),
		}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
}
