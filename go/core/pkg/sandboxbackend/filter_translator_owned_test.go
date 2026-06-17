package sandboxbackend_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/agentsxk8s"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
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

	t.Run("substrate SandboxAgent drops agent-sandbox GVKs", func(t *testing.T) {
		require.NoError(t, atev1alpha1.AddToScheme(scheme))
		routing := sandboxbackend.NewRoutingBackend(agentsxk8s.New(), substrate.NewAgentsBackend(nil, nil))
		allWithSubstrate := append(allTypes, &atev1alpha1.ActorTemplate{})
		sa := &v1alpha2.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
			Spec: v1alpha2.SandboxAgentSpec{
				Platform: v1alpha2.SandboxPlatformSubstrate,
			},
		}
		out, err := sandboxbackend.FilterTranslatorOwnedTypesForList(cl, sa, allWithSubstrate, routing)
		require.NoError(t, err)
		require.Len(t, out, 3)
		var sawSandbox, sawActorTemplate bool
		for _, o := range out {
			if _, ok := o.(*agentsandboxv1.Sandbox); ok {
				sawSandbox = true
			}
			if _, ok := o.(*atev1alpha1.ActorTemplate); ok {
				sawActorTemplate = true
			}
		}
		require.False(t, sawSandbox, "substrate agents must not list agent-sandbox Sandbox resources")
		require.True(t, sawActorTemplate)
	})

	t.Run("agent-sandbox SandboxAgent keeps Sandbox GVK only", func(t *testing.T) {
		require.NoError(t, atev1alpha1.AddToScheme(scheme))
		routing := sandboxbackend.NewRoutingBackend(agentsxk8s.New(), substrate.NewAgentsBackend(nil, nil))
		allWithSubstrate := append(allTypes, &atev1alpha1.ActorTemplate{})
		sa := &v1alpha2.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
			Spec: v1alpha2.SandboxAgentSpec{
				Platform: v1alpha2.SandboxPlatformAgentSandbox,
			},
		}
		out, err := sandboxbackend.FilterTranslatorOwnedTypesForList(cl, sa, allWithSubstrate, routing)
		require.NoError(t, err)
		require.Len(t, out, 3)
		var sawSandbox, sawActorTemplate bool
		for _, o := range out {
			if _, ok := o.(*agentsandboxv1.Sandbox); ok {
				sawSandbox = true
			}
			if _, ok := o.(*atev1alpha1.ActorTemplate); ok {
				sawActorTemplate = true
			}
		}
		require.True(t, sawSandbox)
		require.False(t, sawActorTemplate, "agent-sandbox agents must not list substrate ActorTemplate resources")
	})
}
