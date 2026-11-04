package e2e_test

import (
	"bytes"
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// CI explicitly grants the kagent atespace access to the two shared namespaces.
// kagent-untrusted is deliberately excluded from the credential-provider grant.
func TestCrossNamespaceModelCredentialDelivery(t *testing.T) {
	kube := interactionKubeClient(t)
	for _, name := range []string{"kagent-shared-models", "kagent-untrusted"} {
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		require.NoError(t, kube.Create(t.Context(), namespace))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), namespace)) })
	}
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		for _, namespace := range []string{"kagent-shared-models", "kagent-untrusted"} {
			t.Run(namespace, func(t *testing.T) {
				const token = "cross-namespace-model-token"
				header, want := "Authorization", "Bearer "+token
				if harness.name == claudeE2EHarness {
					header, want = "X-Api-Key", token
				}
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "shared-auth-", Namespace: namespace}, StringData: map[string]string{"token": token}}
				require.NoError(t, kube.Create(t.Context(), secret))
				t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), secret)) })
				decoy := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secret.Name, Namespace: "kagent"}, StringData: map[string]string{"token": "wrong-local-token"}}
				require.NoError(t, kube.Create(t.Context(), decoy))
				t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), decoy)) })
				local := harness.createModel(t, kube, reachableModelURL(t, startCredentialMock(t, header, want)), nil)
				shared := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{GenerateName: "shared-model-", Namespace: namespace}, Spec: *local.Spec.DeepCopy()}
				shared.Spec.APIKeySecret, shared.Spec.APIKeySecretKey = secret.Name, "token"
				shared.Spec.AllowedNamespaces = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}
				require.NoError(t, kube.Create(t.Context(), shared))
				t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), shared)) })
				template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "cross-namespace-model-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: shared.Name, Namespace: namespace}, SystemPrompt: "Reply briefly."}}
				createAndWaitInteractionTemplate(t, harness, kube, template)
				fixture := newInteractionFixtureForTemplate(t, harness, interactionTarget(t), template.Name)
				_, _, task := fixture.send(t, "What is 2+2?")
				if namespace == "kagent-untrusted" {
					require.Equal(t, a2atype.TaskStateFailed, task.Status.State)
				} else {
					require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
				}
			})
		}
	})
}

func TestCrossNamespaceMCPCredentialDelivery(t *testing.T) {
	kube := interactionKubeClient(t)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kagent-shared-tools"}}
	require.NoError(t, kube.Create(t.Context(), namespace))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), namespace)) })
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		const token = "cross-namespace-mcp-token"
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "shared-auth-", Namespace: namespace.Name}, StringData: map[string]string{"token": token}}
		require.NoError(t, kube.Create(t.Context(), secret))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), secret)) })
		decoy := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secret.Name, Namespace: "kagent"}, StringData: map[string]string{"token": "wrong-local-token"}}
		require.NoError(t, kube.Create(t.Context(), decoy))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), decoy)) })
		url, mock := startMCPMock(t)
		server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{GenerateName: "shared-tools-", Namespace: namespace.Name}, Spec: v1alpha3.RemoteMCPServerSpec{
			URL: url, Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			AllowedNamespaces: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll},
			HeadersFrom:       []v1alpha3.ValueRef{{Name: "X-Shared-Token", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: secret.Name, Key: "token"}}},
		}}
		template, _ := createMCPReferenceTemplate(t, harness, server, false)
		fixture := newInteractionFixtureForTemplate(t, harness, interactionTarget(t), template)
		_, _, task := fixture.send(t, "Add 3 and 5 using the configured MCP server.")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		for _, request := range mock.Requests() {
			if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) {
				require.Equal(t, token, request.Headers.Get("X-Shared-Token"))
				return
			}
		}
		t.Fatal("MCP mock received no tool call")
	})
}
