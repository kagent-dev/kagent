package agentsxk8s

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsandboxv1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
)

func TestBackend_BuildSandbox_emitsSandbox(t *testing.T) {
	b := New()
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "a1", Namespace: "ns1", Labels: map[string]string{"app": "x"}},
	}
	pt := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"kagent": "a1"}},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "kagent", Image: "img:v1"}},
		},
	}
	objs, err := b.BuildSandbox(context.Background(), sandboxbackend.BuildInput{
		Agent:       agent,
		PodTemplate: pt,
	})
	require.NoError(t, err)
	require.Len(t, objs, 1)

	sb, ok := objs[0].(*agentsandboxv1.Sandbox)
	require.True(t, ok)
	require.Equal(t, "a1", sb.Name)
	require.Equal(t, "ns1", sb.Namespace)
	require.Equal(t, "img:v1", sb.Spec.PodTemplate.Spec.Containers[0].Image)
	require.NotNil(t, sb.Spec.Replicas)
	require.Equal(t, int32(1), *sb.Spec.Replicas)
}
