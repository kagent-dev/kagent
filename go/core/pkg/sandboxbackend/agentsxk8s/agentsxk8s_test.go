package agentsxk8s

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	extensionsv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

func TestBackend_BuildSandbox_templateAndClaim(t *testing.T) {
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
		Agent:        agent,
		PodTemplate:  pt,
		TemplateName: "kagent-a1",
	})
	require.NoError(t, err)
	require.Len(t, objs, 2)

	st, ok := objs[0].(*extensionsv1alpha1.SandboxTemplate)
	require.True(t, ok)
	require.Equal(t, "kagent-a1", st.Name)
	require.Equal(t, "img:v1", st.Spec.PodTemplate.Spec.Containers[0].Image)
	require.Equal(t, extensionsv1alpha1.NetworkPolicyManagementUnmanaged, st.Spec.NetworkPolicyManagement)
	require.Nil(t, st.Spec.NetworkPolicy)

	claim, ok := objs[1].(*extensionsv1alpha1.SandboxClaim)
	require.True(t, ok)
	require.Equal(t, "a1", claim.Name)
	require.Equal(t, "kagent-a1", claim.Spec.TemplateRef.Name)
}
