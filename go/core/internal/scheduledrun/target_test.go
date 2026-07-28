package scheduledrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestTargetRefResolution(t *testing.T) {
	apiGroup := TargetAPIGroup
	ref := corev1.TypedLocalObjectReference{
		APIGroup: &apiGroup,
		Kind:     TargetKindAgent,
		Name:     "target",
	}

	assert.NoError(t, ValidateTargetRef(ref))
	assert.Equal(t, types.NamespacedName{Namespace: "source", Name: "target"}, TargetKey("source", ref))
	assert.Equal(t, "kagent.dev/Agent/source/target", TargetRefKey("source", ref))

	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "source"},
		Spec:       v1alpha2.ScheduledRunSpec{TargetRef: ref},
	}
	assert.Equal(t, []string{"kagent.dev/Agent/source/target"}, IndexTargetRef(sr))
}

func TestGetTargetUsesReferenceKind(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	metadata := metav1.ObjectMeta{Namespace: "default", Name: "same-name"}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&v1alpha2.Agent{ObjectMeta: metadata},
			&v1alpha2.SandboxAgent{ObjectMeta: metadata},
		).
		Build()

	apiGroup := TargetAPIGroup
	target, err := GetTarget(context.Background(), kube, "default", corev1.TypedLocalObjectReference{
		APIGroup: &apiGroup,
		Kind:     TargetKindSandboxAgent,
		Name:     "same-name",
	})
	require.NoError(t, err)
	assert.IsType(t, &v1alpha2.SandboxAgent{}, target)
}

func TestValidateTargetRefRejectsUnsupportedValues(t *testing.T) {
	apiGroup := "example.com"
	err := ValidateTargetRef(corev1.TypedLocalObjectReference{
		APIGroup: &apiGroup,
		Kind:     TargetKindAgent,
		Name:     "target",
	})
	require.ErrorContains(t, err, "unsupported targetRef.apiGroup")

	apiGroup = TargetAPIGroup
	err = ValidateTargetRef(corev1.TypedLocalObjectReference{APIGroup: &apiGroup, Kind: "Other", Name: "target"})
	require.ErrorContains(t, err, "unsupported targetRef.kind")
}
