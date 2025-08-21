package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

func TestNamespacesHandler(t *testing.T) {
	scheme := runtime.NewScheme()

	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func(watchedNamespaces []string) (*handlers.NamespacesHandler, ctrl_client.Client, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		}
		handler := handlers.NewNamespacesHandler(base, watchedNamespaces)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, responseRecorder
	}

	createTestNamespace := func(name string, phase corev1.NamespacePhase) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: corev1.NamespaceStatus{
				Phase: phase,
			},
		}
	}

	t.Run("HandleListNamespaces", func(t *testing.T) {
		t.Run("Success_ListAllNamespaces", func(t *testing.T) {
			// No watched namespaces configured - should list all namespaces
			handler, kubeClient, responseRecorder := setupHandler([]string{})

			// Create test namespaces
			ns1 := createTestNamespace("default", corev1.NamespaceActive)
			ns2 := createTestNamespace("kube-system", corev1.NamespaceActive)
			ns3 := createTestNamespace("test-ns", corev1.NamespaceActive)

			err := kubeClient.Create(context.Background(), ns1)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), ns2)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), ns3)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/namespaces", nil)
			handler.HandleListNamespaces(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var responseNamespaces api.StandardResponse[[]api.NamespaceResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &responseNamespaces)
			require.NoError(t, err)

			// Check that all namespaces are returned
			assert.Len(t, responseNamespaces.Data, 3)
			namespaceNames := make(map[string]api.NamespaceResponse)
			for _, ns := range responseNamespaces.Data {
				namespaceNames[ns.Name] = ns
			}

			// Verify default namespace
			defaultNS := namespaceNames["default"]
			assert.Equal(t, "default", defaultNS.Name)
			assert.Equal(t, "Active", defaultNS.Status)

			// Verify kube-system namespace
			kubeSystemNS := namespaceNames["kube-system"]
			assert.Equal(t, "kube-system", kubeSystemNS.Name)
			assert.Equal(t, "Active", kubeSystemNS.Status)

			// Verify test namespace
			testNS := namespaceNames["test-ns"]
			assert.Equal(t, "test-ns", testNS.Name)
			assert.Equal(t, "Active", testNS.Status)
		})

		t.Run("Success_DifferentNamespacePhases", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler([]string{})

			// Create namespaces in different phases
			activeNS := createTestNamespace("active-ns", corev1.NamespaceActive)
			terminatingNS := createTestNamespace("terminating-ns", corev1.NamespaceTerminating)

			err := kubeClient.Create(context.Background(), activeNS)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), terminatingNS)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/namespaces", nil)
			handler.HandleListNamespaces(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var responseNamespaces api.StandardResponse[[]api.NamespaceResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &responseNamespaces)
			require.NoError(t, err)
			assert.Len(t, responseNamespaces.Data, 2)

			// Check that different phases are returned correctly
			namespaceStatuses := make(map[string]string)
			for _, ns := range responseNamespaces.Data {
				namespaceStatuses[ns.Name] = ns.Status
			}

			assert.Equal(t, "Active", namespaceStatuses["active-ns"])
			assert.Equal(t, "Terminating", namespaceStatuses["terminating-ns"])
		})

		t.Run("Success_ListWatchedNamespaces", func(t *testing.T) {
			// Configure watched namespaces
			watchedNamespaces := []string{"default", "test-ns"}
			handler, kubeClient, responseRecorder := setupHandler(watchedNamespaces)

			// Create test namespaces
			ns1 := createTestNamespace("default", corev1.NamespaceActive)
			ns2 := createTestNamespace("kube-system", corev1.NamespaceActive)
			ns3 := createTestNamespace("test-ns", corev1.NamespaceActive)

			err := kubeClient.Create(context.Background(), ns1)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), ns2)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), ns3)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/namespaces", nil)
			handler.HandleListNamespaces(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var responseNamespaces api.StandardResponse[[]api.NamespaceResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &responseNamespaces)
			require.NoError(t, err)

			// Check that only watched namespaces are returned
			assert.Len(t, responseNamespaces.Data, 2)
			namespaceNames := make([]string, len(responseNamespaces.Data))
			for i, ns := range responseNamespaces.Data {
				namespaceNames[i] = ns.Name
			}
			assert.Contains(t, namespaceNames, "default")
			assert.Contains(t, namespaceNames, "test-ns")
			assert.NotContains(t, namespaceNames, "kube-system")
		})

		t.Run("Success_WatchedNamespaceNotFound", func(t *testing.T) {
			// Configure watched namespaces where some don't exist
			watchedNamespaces := []string{"default", "non-existent", "test-ns"}
			handler, kubeClient, responseRecorder := setupHandler(watchedNamespaces)

			// Create the namespaces except the non-existent one
			ns1 := createTestNamespace("default", corev1.NamespaceActive)
			ns2 := createTestNamespace("test-ns", corev1.NamespaceActive)

			err := kubeClient.Create(context.Background(), ns1)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), ns2)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/namespaces", nil)
			handler.HandleListNamespaces(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var responseNamespaces api.StandardResponse[[]api.NamespaceResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &responseNamespaces)
			require.NoError(t, err)

			// Check that only existing watched namespaces were returned
			assert.Len(t, responseNamespaces.Data, 2)
			namespaceNames := make([]string, len(responseNamespaces.Data))
			for i, ns := range responseNamespaces.Data {
				namespaceNames[i] = ns.Name
			}
			assert.Contains(t, namespaceNames, "default")
			assert.Contains(t, namespaceNames, "test-ns")
			assert.NotContains(t, namespaceNames, "non-existent")
		})

		t.Run("Success_EmptyResult_NoWatchedNamespaces", func(t *testing.T) {
			// Configure watched namespaces but none exist
			watchedNamespaces := []string{"non-existent-1", "non-existent-2"}
			handler, kubeClient, responseRecorder := setupHandler(watchedNamespaces)

			// Create namespaces except ones that we are watching (which should be non-existent)
			ns1 := createTestNamespace("default", corev1.NamespaceActive)
			ns2 := createTestNamespace("test-ns", corev1.NamespaceActive)

			err := kubeClient.Create(context.Background(), ns1)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), ns2)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/namespaces", nil)
			handler.HandleListNamespaces(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var responseNamespaces api.StandardResponse[[]api.NamespaceResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &responseNamespaces)
			require.NoError(t, err)

			// We should get an empty list because we are only watching non-existent namespaces
			assert.Len(t, responseNamespaces.Data, 0)
		})

		t.Run("Success_EmptyResult_NoNamespaces", func(t *testing.T) {
			// No watched namespaces configured, and no namespaces in the cluster
			handler, _, responseRecorder := setupHandler([]string{})

			req := httptest.NewRequest("GET", "/api/namespaces", nil)
			handler.HandleListNamespaces(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var responseNamespaces api.StandardResponse[[]api.NamespaceResponse]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &responseNamespaces)
			require.NoError(t, err)
			assert.Len(t, responseNamespaces.Data, 0)
		})
	})
}
