package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
)

func TestToolServerTypesHandler_NoKmcp(t *testing.T) {
	scheme := runtime.NewScheme()

	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.ToolServerTypesHandler, ctrl_client.Client, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
			Authorizer:         &auth.NoopAuthorizer{},
		}
		handler := handlers.NewToolServerTypesHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, responseRecorder
	}

	t.Run("HandleListToolServers", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/toolservertypes/", nil)
			req = setUser(req, "test-user")
			handler.HandleListToolServerTypes(responseRecorder, req)

			require.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServerTypes api.StandardResponse[[]string]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &toolServerTypes)
			require.NoError(t, err)
			require.Len(t, toolServerTypes.Data, 1)

			// Verify that RemoteMCPServer is a supported tool server type
			toolServer := toolServerTypes.Data[0]
			require.Equal(t, "RemoteMCPServer", toolServer)
		})
	})
}

func TestToolServerTypesHandler_WithKmcp(t *testing.T) {

	scheme := runtime.NewScheme()

	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)
	err = kmcp.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.ToolServerTypesHandler, ctrl_client.Client, *mockErrorResponseWriter) {
		// Add a dummy MCPServer object to make the type known to the RESTMapper
		dummyMCPServer := &kmcp.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dummy-mcp-server",
				Namespace: "default",
			},
		}

		// Create a RESTMapper that knows about the MCPServer type
		restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{kmcp.GroupVersion})
		restMapper.Add(schema.GroupVersionKind{
			Group:   "kagent.dev",
			Version: "v1alpha1",
			Kind:    "MCPServer",
		}, meta.RESTScopeNamespace)

		// Build the fake client with the MCPServer object
		kubeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(dummyMCPServer).
			WithRESTMapper(restMapper).
			Build()

		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
			Authorizer:         &auth.NoopAuthorizer{},
		}
		handler := handlers.NewToolServerTypesHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, responseRecorder
	}

	t.Run("HandleListToolServers", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/toolservertypes/", nil)
			req = setUser(req, "test-user")
			handler.HandleListToolServerTypes(responseRecorder, req)

			require.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServerTypes api.StandardResponse[[]string]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &toolServerTypes)
			require.NoError(t, err)
			require.Len(t, toolServerTypes.Data, 2)

			// Verify that RemoteMCPServer is a supported tool server type
			toolServer := toolServerTypes.Data[0]
			require.Equal(t, "RemoteMCPServer", toolServer)

			// Verify that MCPServer is a supported tool server type
			toolServer = toolServerTypes.Data[1]
			require.Equal(t, "MCPServer", toolServer)
		})
	})
}
