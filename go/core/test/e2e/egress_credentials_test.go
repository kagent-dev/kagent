package e2e_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestEgressPolicyCredentialDelivery(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	const token = "explicit-policy-test-token"
	destination := startAuthenticatedEgressDestination(t, token)
	image := publishSkillImage(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		modelURL, model, runs := startEgressSkillMock(t, harness.skillsRoot(), destination, "credentials")
		config := harness.createModel(t, kube, modelURL, nil)
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-credentials-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: config.Name}, SystemPrompt: "Load the egress skill and run its probe.",
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "egress", Source: v1alpha3.ArtifactSource{OCI: image, Path: "egress"}}},
		}}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-header-", Namespace: "kagent"}, StringData: map[string]string{"token": token}}
		require.NoError(t, kube.Create(t.Context(), secret))
		t.Cleanup(func() { assert.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), secret))) })
		ref := corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}
		var refs []corev1.LocalObjectReference
		for _, headers := range [][]v1alpha3.Header{
			{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &ref}}},
			{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &ref}}},
		} {
			policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-header-", Namespace: "kagent"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: destination, Headers: headers}}}}
			require.NoError(t, kube.Create(t.Context(), policy))
			t.Cleanup(func() { assert.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), policy))) })
			refs = append(refs, corev1.LocalObjectReference{Name: policy.Name})
		}
		run := runs("credentials")
		finish := runEgressSkill(t, fixture, model, run, "initial_DENIED", "authenticated_ALLOWED")
		run.expect(t, "initial", "DENIED")
		agent := &v1alpha3.Agent{}
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: template.Name}, agent))
		agent.Spec.EgressPolicyRefs = refs
		require.NoError(t, kube.Update(t.Context(), agent))
		run.setPhase("authenticated")
		run.expect(t, "authenticated", "ALLOWED")
		// The HTTP server returns success only when both placeholders have been
		// replaced from the Secret, with and without a prefix respectively.
		finish()
	})
}

func startAuthenticatedEgressDestination(t *testing.T, token string) string {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-API-Key") != token {
			http.Error(w, "credential injection required", http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, "EGRESS_DESTINATION_AUTHENTICATED\n")
	}))
	require.NoError(t, server.Listener.Close())
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Listener = listener
	origin := reachableServerURL(t, "http://"+listener.Addr().String(), "")
	parsed, err := url.Parse(origin)
	require.NoError(t, err)
	server.Start()
	t.Cleanup(server.Close)
	// Prove a successful fetch cannot be produced by a missing/unchanged header.
	for _, value := range []string{"", "kagent-credential-injected"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", parsed.Port()), nil)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+value)
		request.Header.Set("X-API-Key", value)
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusForbidden, response.StatusCode)
	}
	return origin
}
