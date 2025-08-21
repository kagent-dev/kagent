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

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/database"
	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"k8s.io/utils/ptr"
)

func TestToolServersHandler(t *testing.T) {
	scheme := runtime.NewScheme()

	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)
	err = kmcp.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.ToolServersHandler, ctrl_client.Client, *database_fake.InMemoryFakeClient, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		dbClient := database_fake.NewClient()
		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
			DatabaseService:    dbClient,
			Authorizer:         &auth.NoopAuthorizer{},
		}
		handler := handlers.NewToolServersHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, dbClient.(*database_fake.InMemoryFakeClient), responseRecorder
	}

	t.Run("HandleListToolServers", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, _, dbClient, responseRecorder := setupHandler()

			// Create test tool servers in database
			toolServer1 := &database.ToolServer{
				Name:        "default/test-toolserver-1",
				GroupKind:   "kagent.dev/RemoteMCPServer",
				Description: "Test tool server 1",
			}
			toolServer2 := &database.ToolServer{
				Name:        "test-ns/test-toolserver-2",
				GroupKind:   "kagent.dev/RemoteMCPServer",
				Description: "Test tool server 2",
			}

			// Store tool servers in database
			_, err := dbClient.StoreToolServer(toolServer1)
			require.NoError(t, err)
			_, err = dbClient.StoreToolServer(toolServer2)
			require.NoError(t, err)

			// Create test tools in database
			tool1 := &database.Tool{
				ID:          "test-tool",
				ServerName:  "default/test-toolserver-1",
				GroupKind:   "kagent.dev/RemoteMCPServer",
				Description: "Test tool",
			}
			err = dbClient.CreateTool(tool1)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/toolservers/", nil)
			req = setUser(req, "test-user")
			handler.HandleListToolServers(responseRecorder, req)

			require.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServers api.StandardResponse[[]api.ToolServerResponse]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &toolServers)
			require.NoError(t, err)
			require.Len(t, toolServers.Data, 2)

			// Verify first tool server response
			toolServer := toolServers.Data[0]
			require.Equal(t, "default/test-toolserver-1", toolServer.Ref)
			require.Len(t, toolServer.DiscoveredTools, 1)
			require.Equal(t, "test-tool", toolServer.DiscoveredTools[0].Name)

			// Verify second tool server response
			toolServer = toolServers.Data[1]
			require.Equal(t, "test-ns/test-toolserver-2", toolServer.Ref)
		})

		t.Run("EmptyList", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/toolservers/", nil)
			req = setUser(req, "test-user")
			handler.HandleListToolServers(responseRecorder, req)

			require.Equal(t, http.StatusOK, responseRecorder.Code)

			var toolServers api.StandardResponse[[]api.ToolServerResponse]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServers)
			require.NoError(t, err)
			require.Len(t, toolServers.Data, 0)
		})
	})

	t.Run("HandleCreateToolServer", func(t *testing.T) {
		t.Run("Success_RemoteMCPServer_StreamableHttp", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-remote-toolserver",
						Namespace: "default",
					},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "Test remote tool server",
						Protocol:    v1alpha2.RemoteMCPServerProtocolStreamableHttp,
						URL:         "https://example.com/streamable",
						HeadersFrom: []v1alpha2.ValueRef{
							{
								Name:  "API-Key",
								Value: "test-key",
							},
						},
						Timeout:          &metav1.Duration{Duration: 30 * time.Second},
						TerminateOnClose: ptr.To(true),
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer api.StandardResponse[v1alpha2.RemoteMCPServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-remote-toolserver", toolServer.Data.Name)
			assert.Equal(t, "default", toolServer.Data.Namespace)
			assert.Equal(t, "Test remote tool server", toolServer.Data.Spec.Description)
			assert.Equal(t, v1alpha2.RemoteMCPServerProtocolStreamableHttp, toolServer.Data.Spec.Protocol)
			assert.Equal(t, "https://example.com/streamable", toolServer.Data.Spec.URL)
			assert.True(t, *toolServer.Data.Spec.TerminateOnClose)
		})

		t.Run("Success_RemoteMCPServer_Sse", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sse-remote-toolserver",
						Namespace: "default",
					},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "Test SSE remote tool server",
						Protocol:    v1alpha2.RemoteMCPServerProtocolSse,
						URL:         "https://example.com/sse",
						HeadersFrom: []v1alpha2.ValueRef{
							{
								Name: "X-API-Key",
								ValueFrom: &v1alpha2.ValueSource{
									Type: v1alpha2.SecretValueSource,
									Name: "api-secret",
									Key:  "api-key",
								},
							},
						},
						Timeout:        &metav1.Duration{Duration: 30 * time.Second},
						SseReadTimeout: &metav1.Duration{Duration: 60 * time.Second},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer api.StandardResponse[v1alpha2.RemoteMCPServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-sse-remote-toolserver", toolServer.Data.Name)
			assert.Equal(t, "default", toolServer.Data.Namespace)
			assert.Equal(t, v1alpha2.RemoteMCPServerProtocolSse, toolServer.Data.Spec.Protocol)
			assert.Equal(t, "https://example.com/sse", toolServer.Data.Spec.URL)
		})

		t.Run("Success_MCPServer_Stdio", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "MCPServer",
				MCPServer: &kmcp.MCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-stdio-toolserver",
						Namespace: "default",
					},
					Spec: kmcp.MCPServerSpec{
						Deployment: kmcp.MCPServerDeployment{
							Image: "my-mcp-server:latest",
							Port:  8080,
							Cmd:   "/usr/local/bin/my-mcp-server",
							Args:  []string{"--config", "/etc/config.yaml"},
							Env: map[string]string{
								"LOG_LEVEL": "info",
							},
						},
						TransportType:  kmcp.TransportTypeStdio,
						StdioTransport: &kmcp.StdioTransport{},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer api.StandardResponse[kmcp.MCPServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-stdio-toolserver", toolServer.Data.Name)
			assert.Equal(t, "default", toolServer.Data.Namespace)
			assert.Equal(t, "my-mcp-server:latest", toolServer.Data.Spec.Deployment.Image)
			assert.Equal(t, uint16(8080), toolServer.Data.Spec.Deployment.Port)
			assert.Equal(t, kmcp.TransportTypeStdio, toolServer.Data.Spec.TransportType)
		})

		t.Run("Success_DefaultNamespace", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-toolserver",
						// No namespace specified
					},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "Test tool server",
						URL:         "https://example.com/test",
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			defaultNamespace := common.GetResourceNamespace()
			var toolServer api.StandardResponse[v1alpha2.RemoteMCPServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, defaultNamespace, toolServer.Data.Namespace)
		})

		t.Run("InvalidType", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "InvalidType",
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingRemoteMCPServerData", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				// RemoteMCPServer is nil
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingMCPServerData", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "MCPServer",
				// MCPServer is nil
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("ToolServerAlreadyExists", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler()

			// Create existing tool server
			existingToolServer := &v1alpha2.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha2.RemoteMCPServerSpec{
					Description: "Existing tool server",
					URL:         "https://example.com/existing",
				},
			}
			err := kubeClient.Create(context.Background(), existingToolServer)
			require.NoError(t, err)

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-toolserver",
						Namespace: "default",
					},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "New tool server",
						URL:         "https://example.com/new",
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusInternalServerError, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleDeleteToolServer", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, kubeClient, dbClient, responseRecorder := setupHandler()

			// Create tool server to delete
			toolServer := &v1alpha2.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolserver",
					Namespace: "default",
				},
				Spec: v1alpha2.RemoteMCPServerSpec{
					Description: "Tool server to delete",
					URL:         "https://example.com/delete",
				},
			}

			err := kubeClient.Create(context.Background(), toolServer)
			require.NoError(t, err)

			_, err = dbClient.StoreToolServer(&database.ToolServer{
				Name:      "default/test-toolserver",
				GroupKind: "RemoteMCPServer.kagent.dev",
			})
			require.NoError(t, err)

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/test-toolserver", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/toolservers/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteToolServer(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			require.Equal(t, http.StatusOK, responseRecorder.Code, responseRecorder.Body.String())
		})

		t.Run("NotFound", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/nonexistent", nil)
			req = setUser(req, "test-user")

			router := mux.NewRouter()
			router.HandleFunc("/api/toolservers/{namespace}/{name}", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleDeleteToolServer(responseRecorder, r)
			}).Methods("DELETE")

			router.ServeHTTP(responseRecorder, req)

			require.Equal(t, http.StatusNotFound, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingNamespaceParam", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			// Request without namespace param should fail
			req := httptest.NewRequest("DELETE", "/api/toolservers/", nil)
			req = setUser(req, "test-user")
			handler.HandleDeleteToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingToolServerNameParam", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("DELETE", "/api/toolservers/default/", nil)
			req = mux.SetURLVars(req, map[string]string{
				"namespace":      "default",
				"toolServerName": "",
			})
			req = setUser(req, "test-user")

			// Call handler directly
			handler.HandleDeleteToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})
	})
}
