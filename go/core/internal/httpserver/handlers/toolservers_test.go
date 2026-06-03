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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
)

// denyAuthorizer satisfies pkgauth.Authorizer by refusing every Check.
// Used to pin the authorization gate on the create endpoints: a request
// from an unauthorized caller must surface a 403 BEFORE the handler
// reaches KubeClient.Create or createOrUpdateCompanionSecrets.
type denyAuthorizer struct{}

func (denyAuthorizer) Check(_ context.Context, _ pkgauth.Principal, _ pkgauth.Verb, _ pkgauth.Resource) error {
	return assert.AnError
}

var _ pkgauth.Authorizer = denyAuthorizer{}

func TestToolServersHandler(t *testing.T) {
	scheme := runtime.NewScheme()

	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func(t *testing.T) (*handlers.ToolServersHandler, ctrl_client.Client, database.Client, *mockErrorResponseWriter) {
		// Create a RESTMapper that knows about the MCPServer type
		restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{v1alpha1.GroupVersion})
		restMapper.Add(schema.GroupVersionKind{
			Group:   "kagent.dev",
			Version: "v1alpha1",
			Kind:    "MCPServer",
		}, meta.RESTScopeNamespace)

		kubeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRESTMapper(restMapper).
			Build()
		dbClient := setupTestDBClient(t)
		base := &handlers.Base{
			KubeClient:         kubeClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
			DatabaseService:    dbClient,
			Authorizer:         &auth.NoopAuthorizer{},
		}
		// Initialize the toolServerTypes by calling NewToolServerTypesHandler
		_ = handlers.NewToolServerTypesHandler(base)
		handler := handlers.NewToolServersHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, kubeClient, dbClient, responseRecorder
	}

	t.Run("HandleListToolServers", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, _, dbClient, responseRecorder := setupHandler(t)

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
			_, err := dbClient.StoreToolServer(context.Background(), toolServer1)
			require.NoError(t, err)
			_, err = dbClient.StoreToolServer(context.Background(), toolServer2)
			require.NoError(t, err)

			err = dbClient.RefreshToolsForServer(context.Background(), "default/test-toolserver-1", "kagent.dev/RemoteMCPServer",
				&v1alpha2.MCPTool{
					Name:        "test-tool",
					Description: "Test tool",
				},
			)
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
			handler, _, _, responseRecorder := setupHandler(t)

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
			handler, _, _, responseRecorder := setupHandler(t)

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
						TerminateOnClose: new(true),
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
			handler, _, _, responseRecorder := setupHandler(t)

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
			handler, _, _, responseRecorder := setupHandler(t)

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "MCPServer",
				MCPServer: &v1alpha1.MCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-stdio-toolserver",
						Namespace: "default",
					},
					Spec: v1alpha1.MCPServerSpec{
						Deployment: v1alpha1.MCPServerDeployment{
							Image: "my-mcp-server:latest",
							Port:  8080,
							Cmd:   "/usr/local/bin/my-mcp-server",
							Args:  []string{"--config", "/etc/config.yaml"},
							Env: map[string]string{
								"LOG_LEVEL": "info",
							},
						},
						TransportType:  v1alpha1.TransportTypeStdio,
						StdioTransport: &v1alpha1.StdioTransport{},
					},
				},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			var toolServer api.StandardResponse[v1alpha1.MCPServer]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &toolServer)
			require.NoError(t, err)
			assert.Equal(t, "test-stdio-toolserver", toolServer.Data.Name)
			assert.Equal(t, "default", toolServer.Data.Namespace)
			assert.Equal(t, "my-mcp-server:latest", toolServer.Data.Spec.Deployment.Image)
			assert.Equal(t, uint16(8080), toolServer.Data.Spec.Deployment.Port)
			assert.Equal(t, v1alpha1.TransportTypeStdio, toolServer.Data.Spec.TransportType)
		})

		t.Run("Success_DefaultNamespace", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler(t)

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
			handler, _, _, responseRecorder := setupHandler(t)

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
			handler, _, _, responseRecorder := setupHandler(t)

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
			handler, _, _, responseRecorder := setupHandler(t)

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
			handler, _, _, responseRecorder := setupHandler(t)

			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		// SecretMaterials companion-Secret support mirrors the ModelConfig
		// inline-Secret pattern so operators can create an RMS/MCPServer
		// and its referenced Secrets in a single POST without
		// pre-creating Secret objects out of band.
		t.Run("Success_RemoteMCPServer_WithSecretMaterials_CreatesCASecret", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "corp-mcp", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "Corp-CA MCP",
						URL:         "https://mcp.corp.internal/mcp",
						TLS: &v1alpha2.TLSConfig{
							CACertSecretRef: "corp-ca",
							CACertSecretKey: "ca.crt",
						},
					},
				},
				Secrets: []api.SecretMaterial{
					{Name: "corp-ca", Key: "ca.crt", Value: "FAKE PEM"},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)
			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			// Companion Secret created in the same namespace.
			secret := &corev1.Secret{}
			err := kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "corp-ca"}, secret)
			require.NoError(t, err)
			assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
			assert.Equal(t, []byte("FAKE PEM"), secret.Data["ca.crt"])

			// OwnerReference points back at the RMS so K8s GC cleans it up.
			require.Len(t, secret.OwnerReferences, 1)
			or := secret.OwnerReferences[0]
			assert.Equal(t, "RemoteMCPServer", or.Kind)
			assert.Equal(t, "corp-mcp", or.Name)
			assert.Equal(t, v1alpha2.GroupVersion.Identifier(), or.APIVersion)
		})

		t.Run("Success_MCPServer_WithSecretMaterials_CreatesEnvSecret", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "MCPServer",
				MCPServer: &v1alpha1.MCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "kmcp-with-creds", Namespace: "default"},
					Spec: v1alpha1.MCPServerSpec{
						Deployment: v1alpha1.MCPServerDeployment{
							Image: "example/kmcp:latest",
							Port:  8080,
							Cmd:   "/bin/serve",
							SecretRefs: []corev1.LocalObjectReference{
								{Name: "kmcp-creds"},
							},
						},
					},
				},
				Secrets: []api.SecretMaterial{
					{Name: "kmcp-creds", Key: "API_TOKEN", Value: "shhh"},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)
			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			secret := &corev1.Secret{}
			err := kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "kmcp-creds"}, secret)
			require.NoError(t, err)
			assert.Equal(t, []byte("shhh"), secret.Data["API_TOKEN"])
			require.Len(t, secret.OwnerReferences, 1)
			or := secret.OwnerReferences[0]
			assert.Equal(t, "MCPServer", or.Kind)
			assert.Equal(t, "kmcp-with-creds", or.Name)
		})

		t.Run("SecretMaterial_GroupsMultipleKeysIntoSingleSecret", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "multi-secret-mcp", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "RMS with multi-key Secret",
						URL:         "https://mcp.corp.internal/mcp",
					},
				},
				Secrets: []api.SecretMaterial{
					{Name: "shared", Key: "ca.crt", Value: "PEM"},
					{Name: "shared", Key: "token", Value: "abc"},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)
			require.Equal(t, http.StatusCreated, responseRecorder.Code)

			secret := &corev1.Secret{}
			err := kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "shared"}, secret)
			require.NoError(t, err)
			assert.Equal(t, []byte("PEM"), secret.Data["ca.crt"])
			assert.Equal(t, []byte("abc"), secret.Data["token"])
		})

		t.Run("SecretMaterial_InvalidName_Rejected", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler(t)

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "rms-invalid-secret", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "x", URL: "https://x/y",
					},
				},
				Secrets: []api.SecretMaterial{
					{Name: "INVALID NAME WITH SPACES", Key: "ca.crt", Value: "x"},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)
			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("SecretMaterial_ExistingSecretNotOwned_Rejected", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)

			// Pre-create a Secret that isn't owned by any RMS.
			preexisting := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "stranger", Namespace: "default"},
				Type:       corev1.SecretTypeOpaque,
				Data:       map[string][]byte{"ca.crt": []byte("OLD")},
			}
			require.NoError(t, kubeClient.Create(context.Background(), preexisting))

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "stranger-rms", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "x", URL: "https://x/y",
					},
				},
				Secrets: []api.SecretMaterial{
					{Name: "stranger", Key: "ca.crt", Value: "NEW"},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)
			// The 400 surfaces from companionSecretAPIError when the
			// existing Secret isn't already owned by this RMS.
			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)

			// Confirm the unrelated Secret wasn't mutated.
			fresh := &corev1.Secret{}
			err := kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "stranger"}, fresh)
			require.NoError(t, err)
			assert.Equal(t, []byte("OLD"), fresh.Data["ca.crt"])

			// Companion-secret failure must roll back the RMS so the
			// operator's retry doesn't hit AlreadyExists. Pins the
			// partial-failure fix; without rollback the orphan would
			// be readable here.
			orphan := &v1alpha2.RemoteMCPServer{}
			err = kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "stranger-rms"}, orphan)
			assert.True(t, apierrors.IsNotFound(err),
				"RMS must be rolled back when companion-secret creation fails; got err=%v", err)
		})

		// CompanionSecretFailure_RollsBackMCPServer pins the symmetric
		// rollback behavior on the kmcp MCPServer create path so a
		// regression on either branch surfaces in CI.
		t.Run("CompanionSecretFailure_RollsBackMCPServer", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)

			preexisting := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "stranger-kmcp", Namespace: "default"},
				Type:       corev1.SecretTypeOpaque,
				Data:       map[string][]byte{"x": []byte("OLD")},
			}
			require.NoError(t, kubeClient.Create(context.Background(), preexisting))

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "MCPServer",
				MCPServer: &v1alpha1.MCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "stranger-mcp", Namespace: "default"},
					Spec: v1alpha1.MCPServerSpec{
						Deployment: v1alpha1.MCPServerDeployment{
							Image: "example/kmcp:latest",
							Port:  8080,
							Cmd:   "/bin/serve",
						},
					},
				},
				Secrets: []api.SecretMaterial{
					{Name: "stranger-kmcp", Key: "x", Value: "NEW"},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateToolServer(responseRecorder, req)
			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)

			orphan := &v1alpha1.MCPServer{}
			err := kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "stranger-mcp"}, orphan)
			assert.True(t, apierrors.IsNotFound(err),
				"MCPServer must be rolled back when companion-secret creation fails; got err=%v", err)
		})

		// AuthorizationRequired_RemoteMCPServer pins the authz gate on the
		// RMS create path. A caller the authorizer rejects must get a 403
		// AND no RMS, no companion Secret should land in the cluster.
		t.Run("AuthorizationRequired_RemoteMCPServer", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)
			// Swap the Noop authorizer for one that denies every Check.
			handler.Authorizer = denyAuthorizer{}

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "RemoteMCPServer",
				RemoteMCPServer: &v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "denied-rms", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						Description: "should not be created",
						URL:         "https://x/y",
					},
				},
				Secrets: []api.SecretMaterial{
					{Name: "denied-rms-ca", Key: "ca.crt", Value: "PEM"},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "unauthorized-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusForbidden, responseRecorder.Code,
				"unauthorized RMS create must surface 403")
			// Neither the RMS nor the companion Secret should have been
			// created — the authz gate fires before any KubeClient.Create.
			rms := &v1alpha2.RemoteMCPServer{}
			err := kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "denied-rms"}, rms)
			assert.Error(t, err, "denied request must not create the RemoteMCPServer")
			secret := &corev1.Secret{}
			err = kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "denied-rms-ca"}, secret)
			assert.Error(t, err, "denied request must not create the companion Secret")
		})

		// AuthorizationRequired_MCPServer pins the symmetric authz gate
		// on the kmcp MCPServer create path, so a regression on either
		// branch surfaces in the test suite.
		t.Run("AuthorizationRequired_MCPServer", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)
			handler.Authorizer = denyAuthorizer{}

			reqBody := &handlers.ToolServerCreateRequest{
				Type: "MCPServer",
				MCPServer: &v1alpha1.MCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "denied-mcp", Namespace: "default"},
					Spec: v1alpha1.MCPServerSpec{
						Deployment: v1alpha1.MCPServerDeployment{
							Image: "example/kmcp:latest",
							Port:  8080,
							Cmd:   "/bin/serve",
						},
					},
				},
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/toolservers/", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "unauthorized-user")

			handler.HandleCreateToolServer(responseRecorder, req)

			assert.Equal(t, http.StatusForbidden, responseRecorder.Code,
				"unauthorized MCPServer create must surface 403")
			mcp := &v1alpha1.MCPServer{}
			err := kubeClient.Get(context.Background(),
				ctrl_client.ObjectKey{Namespace: "default", Name: "denied-mcp"}, mcp)
			assert.Error(t, err, "denied request must not create the MCPServer")
		})

		t.Run("ToolServerAlreadyExists", func(t *testing.T) {
			handler, kubeClient, _, responseRecorder := setupHandler(t)

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
			handler, kubeClient, dbClient, responseRecorder := setupHandler(t)

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

			_, err = dbClient.StoreToolServer(context.Background(), &database.ToolServer{
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
			handler, _, _, responseRecorder := setupHandler(t)

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
			handler, _, _, responseRecorder := setupHandler(t)

			// Request without namespace param should fail
			req := httptest.NewRequest("DELETE", "/api/toolservers/", nil)
			req = setUser(req, "test-user")
			handler.HandleDeleteToolServer(responseRecorder, req)

			require.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			require.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingToolServerNameParam", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler(t)

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
