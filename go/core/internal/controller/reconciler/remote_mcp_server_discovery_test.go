package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
)

// recordingToolServerStore is the slice of database.Client the RemoteMCPServer
// reconciler touches when discovery is disabled. Every other method panics
// (nil interface), which is the assertion that nothing else is reached.
type recordingToolServerStore struct {
	database.Client
	stored    *database.ToolServer
	refreshed *struct {
		name, groupKind string
		tools           []*v1alpha2.MCPTool
	}
}

func (s *recordingToolServerStore) StoreToolServer(_ context.Context, toolServer *database.ToolServer) (*database.ToolServer, error) {
	s.stored = toolServer
	return toolServer, nil
}

func (s *recordingToolServerStore) RefreshToolsForServer(_ context.Context, serverName string, groupKind string, tools ...*v1alpha2.MCPTool) error {
	s.refreshed = &struct {
		name, groupKind string
		tools           []*v1alpha2.MCPTool
	}{serverName, groupKind, tools}
	return nil
}

// TestReconcileKagentRemoteMCPServer_DiscoveryDisabled verifies that a
// RemoteMCPServer labeled kagent.dev/discovery=disabled is Accepted without the
// controller connecting to it: the URL below is unroutable, so any dial would
// fail the reconcile, and the recording store panics on anything but the two
// catalog writes the opt-out performs.
func TestReconcileKagentRemoteMCPServer_DiscoveryDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	server := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "per-caller",
			Namespace:  "default",
			Generation: 2,
			Labels:     map[string]string{consts.DiscoveryLabel: consts.DiscoveryDisabled},
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "authenticates every caller",
			URL:         "http://192.0.2.1:1/mcp",
			Protocol:    v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
		Status: v1alpha2.RemoteMCPServerStatus{
			DiscoveredTools: []*v1alpha2.MCPTool{{Name: "stale"}},
			Conditions: []metav1.Condition{{
				Type: v1alpha2.AgentConditionTypeAccepted, Status: metav1.ConditionFalse, Reason: "ReconcileFailed", Message: "Unauthorized",
			}},
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(server).
		WithObjects(server).
		Build()
	store := &recordingToolServerStore{}
	reconciler := &kagentReconciler{kube: kube, dbClient: store}

	require.NoError(t, reconciler.ReconcileKagentRemoteMCPServer(context.Background(),
		reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "per-caller"}}))

	updated := &v1alpha2.RemoteMCPServer{}
	require.NoError(t, kube.Get(context.Background(), client.ObjectKeyFromObject(server), updated))

	accepted := meta.FindStatusCondition(updated.Status.Conditions, v1alpha2.AgentConditionTypeAccepted)
	require.NotNil(t, accepted)
	assert.Equal(t, metav1.ConditionTrue, accepted.Status)
	assert.Equal(t, "DiscoveryDisabled", accepted.Reason)
	assert.Equal(t, remoteMCPServerDiscoveryDisabledMessage, accepted.Message)
	assert.Equal(t, server.Generation, accepted.ObservedGeneration)
	assert.Equal(t, server.Generation, updated.Status.ObservedGeneration)
	assert.Empty(t, updated.Status.DiscoveredTools, "the stale inventory is cleared")

	require.NotNil(t, store.stored)
	assert.Equal(t, "default/per-caller", store.stored.Name)
	assert.Equal(t, "authenticates every caller", store.stored.Description)
	require.NotNil(t, store.refreshed)
	assert.Equal(t, store.stored.Name, store.refreshed.name)
	assert.Equal(t, store.stored.GroupKind, store.refreshed.groupKind)
	assert.Empty(t, store.refreshed.tools)
}

func TestRemoteMCPServerDiscoveryDisabled(t *testing.T) {
	for name, tc := range map[string]struct {
		labels map[string]string
		want   bool
	}{
		"no labels":       {labels: nil, want: false},
		"other label":     {labels: map[string]string{"app": "x"}, want: false},
		"other value":     {labels: map[string]string{consts.DiscoveryLabel: "enabled"}, want: false},
		"disabled":        {labels: map[string]string{consts.DiscoveryLabel: consts.DiscoveryDisabled}, want: true},
		"disabled, mixed": {labels: map[string]string{"app": "x", consts.DiscoveryLabel: consts.DiscoveryDisabled}, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			server := &v1alpha2.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Labels: tc.labels}}
			assert.Equal(t, tc.want, remoteMCPServerDiscoveryDisabled(server))
		})
	}
}
