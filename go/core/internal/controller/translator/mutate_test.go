package translator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMutateFuncFor_MergesAnnotationsAndLabels(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"keep": "me", "override": "old"},
			Labels:      map[string]string{"keepLabel": "me", "overrideLabel": "old"},
		},
	}
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"override": "new"},
			Labels:      map[string]string{"overrideLabel": "new"},
		},
	}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	assert.Equal(t, "me", existing.Annotations["keep"])
	assert.Equal(t, "new", existing.Annotations["override"])
	assert.Equal(t, "me", existing.Labels["keepLabel"])
	assert.Equal(t, "new", existing.Labels["overrideLabel"])
}

func TestMutateFuncFor_SetsOwnerReferencesWhenDesiredHasThem(t *testing.T) {
	existing := &corev1.ConfigMap{}
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{Name: "owner"}},
		},
	}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	require.Len(t, existing.OwnerReferences, 1)
	assert.Equal(t, "owner", existing.OwnerReferences[0].Name)
}

func TestMutateFuncFor_LeavesOwnerReferencesWhenDesiredHasNone(t *testing.T) {
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{Name: "existing-owner"}},
		},
	}
	desired := &corev1.ConfigMap{}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	require.Len(t, existing.OwnerReferences, 1)
	assert.Equal(t, "existing-owner", existing.OwnerReferences[0].Name)
}

func TestMutateFuncFor_ConfigMap(t *testing.T) {
	existing := &corev1.ConfigMap{
		Data:       map[string]string{"old": "value"},
		BinaryData: map[string][]byte{"oldBin": []byte("x")},
	}
	desired := &corev1.ConfigMap{
		Data:       map[string]string{"new": "value"},
		BinaryData: map[string][]byte{"newBin": []byte("y")},
	}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	assert.Equal(t, desired.Data, existing.Data)
	assert.Equal(t, desired.BinaryData, existing.BinaryData)
}

func TestMutateFuncFor_Secret(t *testing.T) {
	existing := &corev1.Secret{
		StringData: map[string]string{"old": "value"},
		Data:       map[string][]byte{"oldBin": []byte("x")},
	}
	desired := &corev1.Secret{
		StringData: map[string]string{"new": "value"},
		Data:       map[string][]byte{"newBin": []byte("y")},
	}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	assert.Equal(t, desired.StringData, existing.StringData)
	assert.Equal(t, desired.Data, existing.Data)
}

func TestMutateFuncFor_Service(t *testing.T) {
	existing := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports:    []corev1.ServicePort{{Name: "old", Port: 1}},
			Selector: map[string]string{"old": "sel"},
		},
	}
	desired := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports:    []corev1.ServicePort{{Name: "new", Port: 2}},
			Selector: map[string]string{"new": "sel"},
		},
	}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	assert.Equal(t, desired.Spec.Ports, existing.Spec.Ports)
	assert.Equal(t, desired.Spec.Selector, existing.Spec.Selector)
}

func TestMutateFuncFor_ServiceAccount_NoOp(t *testing.T) {
	existing := &corev1.ServiceAccount{Secrets: []corev1.ObjectReference{{Name: "keep-this"}}}
	desired := &corev1.ServiceAccount{}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	// mutateServiceAccount is intentionally a no-op besides existence.
	require.Len(t, existing.Secrets, 1)
	assert.Equal(t, "keep-this", existing.Secrets[0].Name)
}

func TestMutateFuncFor_Deployment(t *testing.T) {
	replicas := int32(3)
	existing := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas:        int32Ptr(1),
			MinReadySeconds: 1,
			Paused:          false,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"old": "label"}},
			},
		},
	}
	desired := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas:        &replicas,
			MinReadySeconds: 5,
			Paused:          true,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"new": "label"}},
			},
		},
	}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	require.NotNil(t, existing.Spec.Replicas)
	assert.Equal(t, int32(3), *existing.Spec.Replicas)
	assert.Equal(t, int32(5), existing.Spec.MinReadySeconds)
	assert.True(t, existing.Spec.Paused)
	assert.Equal(t, desired.Spec.Template.Spec, existing.Spec.Template.Spec)
}

func TestMutateFuncFor_Deployment_NilReplicasPreservesExisting(t *testing.T) {
	existing := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(7)},
	}
	desired := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{Replicas: nil},
	}

	mutate := MutateFuncFor(existing, desired)
	require.NoError(t, mutate())

	require.NotNil(t, existing.Spec.Replicas)
	assert.Equal(t, int32(7), *existing.Spec.Replicas, "replicas should be preserved (e.g. for HPA) when desired is nil")
}

func int32Ptr(i int32) *int32 { return &i }
