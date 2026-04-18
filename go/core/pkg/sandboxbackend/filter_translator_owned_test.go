package sandboxbackend_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/agentsxk8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsandboxv1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
)

func TestFilterTranslatorOwnedTypesForList(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, agentsandboxv1.AddToScheme(scheme))
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	backend := agentsxk8s.New()

	allTypes := []client.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
		&agentsandboxv1.Sandbox{},
	}

	t.Run("plain Agent drops sandbox GVKs", func(t *testing.T) {
		agent := &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"}}
		out, err := sandboxbackend.FilterTranslatorOwnedTypesForList(cl, agent, allTypes, backend)
		require.NoError(t, err)
		require.Len(t, out, 2)
	})

	t.Run("SandboxAgent keeps sandbox GVKs", func(t *testing.T) {
		sa := &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"}}
		out, err := sandboxbackend.FilterTranslatorOwnedTypesForList(cl, sa, allTypes, backend)
		require.NoError(t, err)
		require.Len(t, out, len(allTypes))
	})

	t.Run("nil backend is passthrough", func(t *testing.T) {
		agent := &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"}}
		out, err := sandboxbackend.FilterTranslatorOwnedTypesForList(cl, agent, allTypes, nil)
		require.NoError(t, err)
		require.Len(t, out, len(allTypes))
	})
}
