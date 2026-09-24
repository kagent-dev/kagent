package e2e_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestModelCredentialDelivery(t *testing.T) {
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		const token = "gateway-injection-e2e-token"
		header, wantCredential := "Authorization", "Bearer "+token
		if harness.name == claudeE2EHarness {
			header, wantCredential = "X-Api-Key", token
		}
		target := interactionTarget(t)
		kube := interactionKubeClient(t)
		// Each compiler must deliver the configured credential to its model origin.
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-auth-", Namespace: "kagent"}, StringData: map[string]string{"token": token}}
		require.NoError(t, kube.Create(t.Context(), secret))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), secret)) })
		origin, err := url.Parse(startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json"))
		require.NoError(t, err)
		proxy := httputil.NewSingleHostReverseProxy(origin)
		observed := make(chan string, 16)
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case observed <- r.Header.Get(header):
			default:
			}
			if r.Header.Get(header) != wantCredential {
				http.Error(w, "credential was not injected", http.StatusUnauthorized)
				return
			}
			proxy.ServeHTTP(w, r)
		}))
		require.NoError(t, server.Listener.Close())
		server.Listener, err = net.Listen("tcp", "0.0.0.0:0")
		require.NoError(t, err)
		server.Start()
		t.Cleanup(server.Close)
		model := harness.createModel(t, kube, reachableModelURL(t, server.URL), nil)
		before := model.DeepCopy()
		model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = secret.Name, "token"
		require.NoError(t, kube.Patch(t.Context(), model, ctrlclient.MergeFrom(before)))
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-credentials-", Namespace: "kagent", Labels: harness.labels()},
			Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly."},
		}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		select {
		case authorization := <-observed:
			require.Equal(t, wantCredential, authorization)
		case <-time.After(time.Second):
			t.Fatal("the runtime never reached the credential-checking origin")
		}
	})
}
