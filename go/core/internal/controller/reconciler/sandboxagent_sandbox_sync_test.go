package reconciler

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/agentsxk8s"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsandboxv1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extensionsv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

func TestResyncAgentSandboxWorkload_deletesSandboxWhenTemplateRVChanges(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, extensionsv1alpha1.AddToScheme(scheme))
	require.NoError(t, agentsandboxv1.AddToScheme(scheme))
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	ns := "ns1"
	agName := "myagent"
	tmplName := sandboxTemplateNameForAgent(agName)

	tmpl := &extensionsv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            tmplName,
			ResourceVersion: "rv-2",
		},
		Spec: extensionsv1alpha1.SandboxTemplateSpec{},
	}
	claim := &extensionsv1alpha1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      agName,
			Annotations: map[string]string{
				sandboxTemplateResourceVersionAnnotation: "rv-1",
			},
		},
		Spec: extensionsv1alpha1.SandboxClaimSpec{
			TemplateRef: extensionsv1alpha1.SandboxTemplateRef{Name: tmplName},
		},
	}
	sb := &agentsandboxv1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: agName},
		Spec:       agentsandboxv1.SandboxSpec{},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tmpl, claim, sb).Build()

	r := &kagentReconciler{kube: cl, sandboxBackend: agentsxk8s.New()}
	sa := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: agName},
	}

	require.NoError(t, r.resyncAgentSandboxWorkload(context.Background(), sa))

	err := cl.Get(context.Background(), client.ObjectKeyFromObject(sb), sb)
	require.True(t, apierrors.IsNotFound(err))

	var updatedClaim extensionsv1alpha1.SandboxClaim
	require.NoError(t, cl.Get(context.Background(), client.ObjectKeyFromObject(claim), &updatedClaim))
	require.Equal(t, "rv-2", updatedClaim.Annotations[sandboxTemplateResourceVersionAnnotation])
}
