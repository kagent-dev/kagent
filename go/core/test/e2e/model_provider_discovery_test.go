package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestModelProviderConfigDiscoversModels(t *testing.T) {
	// Given an OpenAI-compatible endpoint that requires a discovery token, when a
	// ModelProviderConfig points at it, assert that the provider becomes Ready,
	// is listed as configured, and returns the endpoint's models.
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	endpoint := reachableServerURL(t, startMCPDiscoveryHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer discovery-token" {
			http.Error(w, "unexpected discovery request", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"team-small"},{"id":"team-large"}]}`))
	})), "/v1")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "model-discovery-", Namespace: "kagent"},
		StringData: map[string]string{"token": "discovery-token"},
	}
	createModelDiscoveryObject(t, kube, secret)
	provider := &v1alpha3.ModelProviderConfig{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "model-discovery-", Namespace: "kagent"},
		Spec: v1alpha3.ModelProviderConfigSpec{
			Type:     v1alpha3.ModelProviderOpenAI,
			Endpoint: endpoint,
			DiscoveryAuth: &v1alpha3.ModelDiscoveryAuth{SecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token",
			}},
		},
	}
	createModelDiscoveryObject(t, kube, provider)

	require.Eventually(t, func() bool {
		if err := kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(provider), provider); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(provider.Status.Conditions, v1alpha3.ModelProviderConfigConditionTypeReady) &&
			provider.Status.ModelCount == 2
	}, time.Minute, time.Second, "ModelProviderConfig must become Ready with the discovered model count")

	models := apiv1.NewModelServiceClient(newControllerConn(t, target))
	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e")
	configured, err := models.ListConfiguredProviders(ctx, &apiv1.ListConfiguredProvidersRequest{})
	require.NoError(t, err)
	var listed *apiv1.ConfiguredProvider
	for _, candidate := range configured.GetProviders() {
		if candidate.GetName() == provider.Name {
			listed = candidate
		}
	}
	require.NotNil(t, listed, "a Ready ModelProviderConfig must be listed as a configured provider")
	require.Equal(t, endpoint, listed.GetEndpoint())

	discovered, err := models.ListProviderModels(ctx, &apiv1.ListProviderModelsRequest{ProviderName: provider.Name, Refresh: true})
	require.NoError(t, err)
	require.Equal(t, []string{"team-large", "team-small"}, discovered.GetModels())
}

func createModelDiscoveryObject(t *testing.T, kube ctrlclient.Client, object ctrlclient.Object) {
	t.Helper()
	require.NoError(t, kube.Create(t.Context(), object))
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), object); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete model discovery fixture: %v", err)
		}
	})
}
