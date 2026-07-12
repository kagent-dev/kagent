package scheduledrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestTargetRefResolution(t *testing.T) {
	apiGroup := v1alpha2.ScheduledRunTargetAPIGroup
	ref := corev1.TypedObjectReference{
		APIGroup: &apiGroup,
		Kind:     v1alpha2.ScheduledRunTargetKindAgent,
		Name:     "target",
	}

	assert.Equal(t, "source", TargetNamespace("source", ref))
	assert.Equal(t, types.NamespacedName{Namespace: "source", Name: "target"}, TargetKey("source", ref))

	ref.Namespace = new("target-ns")
	assert.Equal(t, "target-ns", TargetNamespace("source", ref))
	assert.Equal(t, types.NamespacedName{Namespace: "target-ns", Name: "target"}, TargetKey("source", ref))

	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "source"},
		Spec:       v1alpha2.ScheduledRunSpec{TargetRef: ref},
	}
	assert.Equal(t, []string{"kagent.dev/Agent/target-ns/target"}, IndexTargetRef(sr))
}

func TestValidateTargetNamespaceAccess(t *testing.T) {
	target := &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "target-ns", Name: "target"}}

	err := ValidateTargetNamespaceAccess(context.Background(), nil, "source-ns", target)
	require.EqualError(t, err, "target access denied: cross-namespace reference to Agent target-ns/target is not allowed from namespace source-ns")
	assert.ErrorIs(t, err, ErrTargetAccessDenied)

	target.Spec.AllowedNamespaces = &v1alpha2.AllowedNamespaces{From: v1alpha2.NamespacesFromAll}
	require.NoError(t, ValidateTargetNamespaceAccess(context.Background(), nil, "source-ns", target))
	require.NoError(t, ValidateTargetNamespaceAccess(context.Background(), nil, "target-ns", target))

	sandboxTarget := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Namespace: "target-ns", Name: "sandbox"},
		Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
			AllowedNamespaces: &v1alpha2.AllowedNamespaces{From: v1alpha2.NamespacesFromAll},
		}},
	}
	require.NoError(t, ValidateTargetNamespaceAccess(context.Background(), nil, "source-ns", sandboxTarget))
}
