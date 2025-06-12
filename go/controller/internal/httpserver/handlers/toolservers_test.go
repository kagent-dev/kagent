package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
)

func TestToolServersHandler(t *testing.T) {
	scheme := runtime.NewScheme()
	
	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.ToolServersHandler, client.Client, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		}
		handler := handlers.NewToolServersHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, responseRecorder
	}

	t.Run("HandleListToolServers", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create test tool servers
			toolServer1 := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver-1",
					Namespace: "default",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Test tool server 1",
					Config: v1alpha1.ToolServerConfig{
						Stdio: &v1alpha1.StdioMcpServerConfig{
							Command: "python",
							Args:    []string{"-m", "test_tool"},
							Env: map[string]string{
								"ENV_VAR": "value",
							},
							EnvFrom: []v1alpha1.ValueRef{
								{
									Name:  "API_KEY",
									Value: "test-key",
								},
							},
						},
					},
				},
				Status: v1alpha1.ToolServerStatus{
					DiscoveredTools: []*v1alpha1.MCPTool{
						{
							Name: "test-tool",
							Component: v1alpha1.Component{
								Provider:         "test-provider",
								ComponentType:    "tool",
								Version:          1,
								ComponentVersion: 1,
								Description:      "Test tool",
								Label:            "Test Tool",
							},
						},
					},
				},
			}

			toolServer2 := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver-2",
					Namespace: "test-ns",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Test tool server 2",
					Config: v1alpha1.ToolServerConfig{
						Sse: &v1alpha1.SseMcpServerConfig{
							URL: "https://example.com/sse",
							Headers: map[string]v1alpha1.AnyType{
								"Authorization": {RawMessage: []byte(`"Bearer token"`)},
							},
							Timeout:        "30s",
							SseReadTimeout: "60s",
						},
					},
				},
			}

			err := kubeClient.Create(context.Background(), toolServer1)
			require.NoError(t, err)
			err = kubeClient.Create(context.Background(), toolServer2)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/toolservers/", nil)
			handler.HandleListToolServers(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServers []handlers.ToolServerResponse
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &toolServers)
			require.NoError(t, err)
			assert.Len(t, toolServers, 2)

			// Verify first tool server response
			toolServer := toolServers[0]
			assert.Equal(t, "default/test-toolserver-1", toolServer.Ref)
			assert.NotNil(t, toolServer.Config.Stdio)
			assert.Equal(t, "python", toolServer.Config.Stdio.Command)
			assert.Equal(t, []string{"-m", "test_tool"}, toolServer.Config.Stdio.Args)
			assert.Len(t, toolServer.DiscoveredTools, 1)
			assert.Equal(t, "test-tool", toolServer.DiscoveredTools[0].Name)

			// Verify second tool server response
			toolServer = toolServers[1]
			assert.Equal(t, "test-ns/test-toolserver-2", toolServer.Ref)
			assert.NotNil(t, toolServer.Config.Sse)
			assert.Equal(t, "https://example.com/sse", toolServer.Config.Sse.URL)
		})

		t.Run("EmptyList", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/toolservers/", nil)
			handler.HandleListToolServers(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServers []handlers.ToolServerResponse
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServers)
			require.NoError(t, err)
			assert.Len(t, toolServers, 0)
		})
	})

	t.Run("HandleCreateToolServer", func(t *testing.T) {
		t.Run("Success_Stdio", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Test tool server",
					Config: v1alpha1.ToolServerConfig{
						Stdio: &v1alpha1.StdioMcpServerConfig{
							Command: "python",
							Args:    []string{"-m", "test_tool"},
							Env: map[string]string{
								"API_KEY": "test-key",
							},
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer v1alpha1.ToolServer
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-toolserver", toolServer.Name)
			assert.Equal(t, "default", toolServer.Namespace)
			assert.Equal(t, "Test tool server", toolServer.Spec.Description)
			assert.NotNil(t, toolServer.Spec.Config.Stdio)
			assert.Equal(t, "python", toolServer.Spec.Config.Stdio.Command)
		})

		t.Run("Success_Sse", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sse-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Test SSE tool server",
					Config: v1alpha1.ToolServerConfig{
						Sse: &v1alpha1.SseMcpServerConfig{
							URL: "https://example.com/sse",
							Headers: map[string]v1alpha1.AnyType{
								"Authorization": {RawMessage: []byte(`"Bearer token"`)},
							},
							HeadersFrom: []v1alpha1.ValueRef{
								{
									Name: "X-API-Key",
									ValueFrom: &v1alpha1.ValueSource{
										Type:     v1alpha1.SecretValueSource,
										ValueRef: "api-secret",
										Key:      "api-key",
									},
								},
							},
							Timeout:        "30s",
							SseReadTimeout: "60s",
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer v1alpha1.ToolServer
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-sse-toolserver", toolServer.Name)
			assert.Equal(t, "default", toolServer.Namespace)
			assert.NotNil(t, toolServer.Spec.Config.Sse)
			assert.Equal(t, "https://example.com/sse", toolServer.Spec.Config.Sse.URL)
		})

		t.Run("Success_DefaultNamespace", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-toolserver",
					// No namespace specified
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Test tool server",
					Config: v1alpha1.ToolServerConfig{
						Stdio: &v1alpha1.StdioMcpServerConfig{
							Command: "python",
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			defaultNamespace := common.GetResourceNamespace()
			var toolServer v1alpha1.ToolServer
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, defaultNamespace, toolServer.Namespace)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("ToolServerAlreadyExists", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create existing tool server
			existingToolServer := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Existing tool server",
					Config: v1alpha1.ToolServerConfig{
						Stdio: &v1alpha1.StdioMcpServerConfig{
							Command: "python",
						},
					},
				},
			}
			err := kubeClient.Create(context.Background(), existingToolServer)
			require.NoError(t, err)

			reqBody := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "New tool server",
					Config: v1alpha1.ToolServerConfig{
						Stdio: &v1alpha1.StdioMcpServerConfig{
							Command: "node",
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusInternalServerError, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleDeleteToolServer", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, responseRecorder := setupHandler()

			// Create tool server to delete
			toolServer := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Tool server to delete",
					Config: v1alpha1.ToolServerConfig{
						Stdio: &v1alpha1.StdioMcpServerConfig{
							Command: "python",
						},
					},
				},
			}

			err := kubeClient.Create(context.Background(), toolServer)
			require.NoError(t, err)

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/test-toolserver", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/toolservers/{namespace}/{toolServerName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteToolServer(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusNoContent, responseRecorder.Code)
		})

		t.Run("NotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/nonexistent", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/toolservers/{namespace}/{toolServerName}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteToolServer(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingNamespaceParam", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			// Request without namespace param should fail
			req := httptest.NewRequest("DELETE", "/api/toolservers/", nil)
			handler.HandleDeleteToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingToolServerNameParam", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/", nil)
			req = mux.SetURLVars(req, map[string]string{
				"namespace":      "default",
				"toolServerName": "",
			})

			// Call handler directly
			handler.HandleDeleteToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})
}