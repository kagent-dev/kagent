package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"k8s.io/utils/ptr"
)

func TestToolServersHandler(t *testing.T) {
	scheme := runtime.NewScheme()

	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.ToolServersHandler, ctrl_client.Client, *mockErrorResponseWriter) {
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
						Type: v1alpha1.ToolServerTypeStreamableHttp,
						StreamableHttp: &v1alpha1.StreamableHttpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/streamable",
								HeadersFrom: []v1alpha1.ValueRef{
									{
										Name:  "API_KEY",
										Value: "test-key",
									},
								},
								Timeout: &metav1.Duration{Duration: 30 * time.Second},
							},
						},
					},
				},
				Status: v1alpha1.ToolServerStatus{
					DiscoveredTools: []*v1alpha1.MCPTool{
						{
							Name: "test-tool",
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
						Type: v1alpha1.ToolServerTypeSse,
						Sse: &v1alpha1.SseMcpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/sse",
								HeadersFrom: []v1alpha1.ValueRef{
									{
										Name: "Authorization",
										ValueFrom: &v1alpha1.ValueSource{
											Type:     v1alpha1.SecretValueSource,
											ValueRef: "auth-secret",
											Key:      "token",
										},
									},
								},
								Timeout:        &metav1.Duration{Duration: 30 * time.Second},
								SseReadTimeout: &metav1.Duration{Duration: 60 * time.Second},
							},
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

			require.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServers api.StandardResponse[[]api.ToolServerResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &toolServers)
			require.NoError(t, err)
			require.Len(t, toolServers.Data, 2)

			// Verify first tool server response
			toolServer := toolServers.Data[0]
			require.Equal(t, "default/test-toolserver-1", toolServer.Ref)
			require.Equal(t, v1alpha1.ToolServerTypeStreamableHttp, toolServer.Config.Type)
			require.Equal(t, "https://example.com/streamable", toolServer.Config.StreamableHttp.URL)
			require.Len(t, toolServer.DiscoveredTools, 1)
			require.Equal(t, "test-tool", toolServer.DiscoveredTools[0].Name)

			// Verify second tool server response
			toolServer = toolServers.Data[1]
			require.Equal(t, "test-ns/test-toolserver-2", toolServer.Ref)
			require.Equal(t, v1alpha1.ToolServerTypeSse, toolServer.Config.Type)
			require.Equal(t, "https://example.com/sse", toolServer.Config.Sse.URL)
		})

		t.Run("EmptyList", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/toolservers/", nil)
			handler.HandleListToolServers(responseRecorder, req)

			require.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServers api.StandardResponse[[]api.ToolServerResponse]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServers)
			require.NoError(t, err)
			require.Len(t, toolServers.Data, 0)
		})
	})

	t.Run("HandleCreateToolServer", func(t *testing.T) {
		t.Run("Success_StreamableHttp", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			reqBody := &v1alpha1.ToolServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha1.ToolServerSpec{
					Description: "Test tool server",
					Config: v1alpha1.ToolServerConfig{
						Type: v1alpha1.ToolServerTypeStreamableHttp,
						StreamableHttp: &v1alpha1.StreamableHttpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/streamable",
								HeadersFrom: []v1alpha1.ValueRef{
									{
										Name:  "API-Key",
										Value: "test-key",
									},
								},
								Timeout: &metav1.Duration{Duration: 30 * time.Second},
							},
							TerminateOnClose: ptr.To(true),
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer api.StandardResponse[v1alpha1.ToolServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-toolserver", toolServer.Data.Name)
			assert.Equal(t, "default", toolServer.Data.Namespace)
			assert.Equal(t, "Test tool server", toolServer.Data.Spec.Description)
			assert.Equal(t, v1alpha1.ToolServerTypeStreamableHttp, toolServer.Data.Spec.Config.Type)
			assert.Equal(t, "https://example.com/streamable", toolServer.Data.Spec.Config.StreamableHttp.URL)
			assert.True(t, *toolServer.Data.Spec.Config.StreamableHttp.TerminateOnClose)
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
						Type: v1alpha1.ToolServerTypeSse,
						Sse: &v1alpha1.SseMcpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/sse",
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
								Timeout:        &metav1.Duration{Duration: 30 * time.Second},
								SseReadTimeout: &metav1.Duration{Duration: 60 * time.Second},
							},
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer api.StandardResponse[v1alpha1.ToolServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-sse-toolserver", toolServer.Data.Name)
			assert.Equal(t, "default", toolServer.Data.Namespace)
			assert.Equal(t, v1alpha1.ToolServerTypeSse, toolServer.Data.Spec.Config.Type)
			assert.Equal(t, "https://example.com/sse", toolServer.Data.Spec.Config.Sse.URL)
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
						Type: v1alpha1.ToolServerTypeStreamableHttp,
						StreamableHttp: &v1alpha1.StreamableHttpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/test",
							},
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			defaultNamespace := common.GetResourceNamespace()
			var toolServer api.StandardResponse[v1alpha1.ToolServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, defaultNamespace, toolServer.Data.Namespace)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
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
						Type: v1alpha1.ToolServerTypeStreamableHttp,
						StreamableHttp: &v1alpha1.StreamableHttpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/existing",
							},
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
						Type: v1alpha1.ToolServerTypeSse,
						Sse: &v1alpha1.SseMcpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/new",
							},
						},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusInternalServerError, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
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
						Type: v1alpha1.ToolServerTypeStreamableHttp,
						StreamableHttp: &v1alpha1.StreamableHttpServerConfig{
							HttpToolServerConfig: v1alpha1.HttpToolServerConfig{
								URL: "https://example.com/delete",
							},
						},
					},
				},
			}

			err := kubeClient.Create(context.Background(), toolServer)
			require.NoError(t, err)

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/test-toolserver", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/toolservers/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteToolServer(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			require.Equal(t, http.StatusOK, responseRecorder.Code, responseRecorder.Body.String())
		})

		t.Run("NotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/nonexistent", nil)

			router := mux.NewRouter()
			router.HandleFunc("/api/toolservers/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteToolServer(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			require.Equal(t, http.StatusNotFound, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingNamespaceParam", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler()

			// Request without namespace param should fail
			req := httptest.NewRequest("DELETE", "/api/toolservers/", nil)
			handler.HandleDeleteToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
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

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})
	})
}
