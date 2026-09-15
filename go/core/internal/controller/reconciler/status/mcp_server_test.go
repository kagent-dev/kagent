package status

import (
	"context"
	"errors"
	"testing"

	"github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	return scheme
}

func conditionOf(mcp *v1alpha1.MCPServer, t v1alpha1.MCPServerConditionType) *metav1.Condition {
	for i := range mcp.Status.Conditions {
		if mcp.Status.Conditions[i].Type == string(t) {
			return &mcp.Status.Conditions[i]
		}
	}
	return nil
}

func TestReconcileMCPServerStatus_ReconcileErrSetsAcceptedFalse(t *testing.T) {
	scheme := newScheme(t)
	mcp := &v1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "default"}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).WithStatusSubresource(mcp).Build()

	requeue, err := ReconcileMCPServerStatus(context.Background(), kube, mcp, errors.New("bad config"))
	require.NoError(t, err)
	assert.False(t, requeue)

	accepted := conditionOf(mcp, v1alpha1.MCPServerConditionAccepted)
	require.NotNil(t, accepted)
	assert.Equal(t, metav1.ConditionFalse, accepted.Status)
	assert.Equal(t, string(v1alpha1.MCPServerReasonInvalidConfig), accepted.Reason)
}

func TestReconcileMCPServerStatus_DeploymentMissing_NotReadyNoRequeue(t *testing.T) {
	scheme := newScheme(t)
	mcp := &v1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "default"}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).WithStatusSubresource(mcp).Build()

	requeue, err := ReconcileMCPServerStatus(context.Background(), kube, mcp, nil)
	require.NoError(t, err)
	assert.False(t, requeue, "deployment not found should not trigger requeue")

	ready := conditionOf(mcp, v1alpha1.MCPServerConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, string(v1alpha1.MCPServerReasonPodsNotReady), ready.Reason)
}

func TestReconcileMCPServerStatus_DeploymentNotFullyAvailable_Requeues(t *testing.T) {
	scheme := newScheme(t)
	mcp := &v1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "default"}}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "default"},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: 1, Replicas: 2},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp, deployment).WithStatusSubresource(mcp).Build()

	requeue, err := ReconcileMCPServerStatus(context.Background(), kube, mcp, nil)
	require.NoError(t, err)
	assert.True(t, requeue)

	ready := conditionOf(mcp, v1alpha1.MCPServerConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, string(v1alpha1.MCPServerReasonNotAvailable), ready.Reason)
}

func TestReconcileMCPServerStatus_DeploymentFullyAvailable_Ready(t *testing.T) {
	scheme := newScheme(t)
	mcp := &v1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "default"}}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "default"},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: 2, Replicas: 2},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp, deployment).WithStatusSubresource(mcp).Build()

	requeue, err := ReconcileMCPServerStatus(context.Background(), kube, mcp, nil)
	require.NoError(t, err)
	assert.False(t, requeue)

	ready := conditionOf(mcp, v1alpha1.MCPServerConditionReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionTrue, ready.Status)
	assert.Equal(t, string(v1alpha1.MCPServerReasonAvailable), ready.Reason)

	accepted := conditionOf(mcp, v1alpha1.MCPServerConditionAccepted)
	require.NotNil(t, accepted)
	assert.Equal(t, metav1.ConditionTrue, accepted.Status)
}

func TestReconcileMCPServerStatus_ObservedGenerationUpdated(t *testing.T) {
	scheme := newScheme(t)
	mcp := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "default", Generation: 5},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mcp).WithStatusSubresource(mcp).Build()

	_, err := ReconcileMCPServerStatus(context.Background(), kube, mcp, nil)
	require.NoError(t, err)

	assert.Equal(t, int64(5), mcp.Status.ObservedGeneration)
}

func TestSetCondition_PreservesLastTransitionTimeWhenStatusUnchanged(t *testing.T) {
	mcp := &v1alpha1.MCPServer{}
	setCondition(mcp, v1alpha1.MCPServerConditionReady, metav1.ConditionTrue, v1alpha1.MCPServerReasonAvailable, "first")
	first := conditionOf(mcp, v1alpha1.MCPServerConditionReady)
	require.NotNil(t, first)
	firstTransition := first.LastTransitionTime

	setCondition(mcp, v1alpha1.MCPServerConditionReady, metav1.ConditionTrue, v1alpha1.MCPServerReasonAvailable, "second message")
	second := conditionOf(mcp, v1alpha1.MCPServerConditionReady)
	require.NotNil(t, second)

	assert.Equal(t, firstTransition, second.LastTransitionTime, "transition time should not change when status is unchanged")
	assert.Equal(t, "second message", second.Message, "message should still update")
	require.Len(t, mcp.Status.Conditions, 1, "should update existing condition, not append")
}

func TestSetCondition_UpdatesTransitionTimeWhenStatusChanges(t *testing.T) {
	mcp := &v1alpha1.MCPServer{}
	setCondition(mcp, v1alpha1.MCPServerConditionReady, metav1.ConditionFalse, v1alpha1.MCPServerReasonPodsNotReady, "not ready")
	setCondition(mcp, v1alpha1.MCPServerConditionReady, metav1.ConditionTrue, v1alpha1.MCPServerReasonAvailable, "now ready")

	cond := conditionOf(mcp, v1alpha1.MCPServerConditionReady)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Len(t, mcp.Status.Conditions, 1)
}
