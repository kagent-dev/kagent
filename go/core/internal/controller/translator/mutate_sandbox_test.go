package translator

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsandboxv1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
)

func TestMutateFuncFor_Sandbox_replacesPodTemplateSlices(t *testing.T) {
	oldImg := "registry.example/old:v1"
	newImg := "registry.example/new:v2"

	existing := &agentsandboxv1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
		Spec: agentsandboxv1.SandboxSpec{
			PodTemplate: agentsandboxv1.PodTemplate{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "agent", Image: oldImg}},
				},
			},
		},
	}
	desired := existing.DeepCopy()
	desired.Spec.PodTemplate.Spec.Containers[0].Image = newImg

	f := MutateFuncFor(existing, desired)
	require.NoError(t, f())
	require.Equal(t, newImg, existing.Spec.PodTemplate.Spec.Containers[0].Image)
}
