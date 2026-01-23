package reconciler

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agenttranslator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
)

// TestReconcileKagentMCPServer_InvalidPort tests that ReconcileKagentMCPServer returns an error
// when the MCPServer has an invalid port configuration. This validates the fix for the issue
// where conversion errors were only logged but not returned.
func TestReconcileKagentMCPServer_InvalidPort(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create an MCPServer with invalid port (0)
	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  0, // Invalid port
			},
			TransportType: "stdio",
		},
	}

	// Create fake client with test objects
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mcpServer).
		Build()

	// Create an in-memory database manager
	dbManager, err := database.NewManager(&database.Config{
		DatabaseType: database.DatabaseTypeSqlite,
		SqliteConfig: &database.SqliteConfig{
			DatabasePath: "file::memory:?cache=shared",
		},
	})
	require.NoError(t, err)
	defer dbManager.Close()

	err = dbManager.Initialize()
	require.NoError(t, err)

	dbClient := database.NewClient(dbManager)

	// Create reconciler
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)
	reconciler := NewKagentReconciler(
		translator,
		kubeClient,
		dbClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
	)

	// Call ReconcileKagentMCPServer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test",
			Name:      "test-mcp-server",
		},
	}

	// Should return an error indicating the port is invalid
	err = reconciler.ReconcileKagentMCPServer(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to convert mcp server")
	assert.Contains(t, err.Error(), "test/test-mcp-server")
	assert.Contains(t, err.Error(), "cannot determine port")

	// Verify the error is a ValidationError
	var validationErr *agenttranslator.ValidationError
	assert.True(t, errors.As(err, &validationErr), "Error should be a ValidationError")
}

// TestReconcileKagentMCPServer_ValidPort tests that ReconcileKagentMCPServer succeeds
// when the MCPServer has a valid port configuration.
func TestReconcileKagentMCPServer_ValidPort(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create an MCPServer with valid port
	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mcp-server",
			Namespace: "test",
		},
		Spec: v1alpha1.MCPServerSpec{
			Deployment: v1alpha1.MCPServerDeployment{
				Image: "test-image:latest",
				Port:  8080, // Valid port
			},
			TransportType: "stdio",
		},
	}

	// Create fake client with test objects
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mcpServer).
		Build()

	// Create an in-memory database manager
	dbManager, err := database.NewManager(&database.Config{
		DatabaseType: database.DatabaseTypeSqlite,
		SqliteConfig: &database.SqliteConfig{
			DatabasePath: "file::memory:?cache=shared",
		},
	})
	require.NoError(t, err)
	defer dbManager.Close()

	err = dbManager.Initialize()
	require.NoError(t, err)

	dbClient := database.NewClient(dbManager)

	// Create reconciler
	translator := agenttranslator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
		nil,
		"",
	)
	reconciler := NewKagentReconciler(
		translator,
		kubeClient,
		dbClient,
		types.NamespacedName{Namespace: "test", Name: "default-model"},
	)

	// Call ReconcileKagentMCPServer
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test",
			Name:      "test-mcp-server",
		},
	}

	// Should succeed (returns error from upsertToolServerForRemoteMCPServer which is expected
	// since we don't have a real MCP server to connect to, but conversion should succeed)
	err = reconciler.ReconcileKagentMCPServer(ctx, req)
	// The error will be from connecting to the MCP server, not from conversion
	if err != nil {
		assert.NotContains(t, err.Error(), "failed to convert mcp server")
	}

	// Verify the tool server was stored in the database
	serverRef := utils.GetObjectRef(mcpServer)
	servers, err := dbClient.ListToolServers()
	require.NoError(t, err)

	found := slices.ContainsFunc(servers, func(server database.ToolServer) bool {
		return server.Name == serverRef
	})
	assert.True(t, found, "MCPServer should be stored in database")
}

// TestReconcileKagentMCPServer_ErrorPropagation tests that errors from conversion
// are properly propagated and not silently swallowed. This is a regression test
// for the original issue where errors were only logged.
func TestReconcileKagentMCPServer_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	scheme := schemev1.Scheme
	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	testCases := []struct {
		name        string
		mcpServer   *v1alpha1.MCPServer
		expectError bool
		errorText   string
	}{
		{
			name: "zero port",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "zero-port-mcp",
					Namespace: "test",
				},
				Spec: v1alpha1.MCPServerSpec{
					Deployment: v1alpha1.MCPServerDeployment{
						Image: "test-image:latest",
						Port:  0,
					},
					TransportType: "stdio",
				},
			},
			expectError: true,
			errorText:   "cannot determine port",
		},
		{
			name: "valid port",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-port-mcp",
					Namespace: "test",
				},
				Spec: v1alpha1.MCPServerSpec{
					Deployment: v1alpha1.MCPServerDeployment{
						Image: "test-image:latest",
						Port:  8080,
					},
					TransportType: "stdio",
				},
			},
			expectError: false,
			errorText:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create fake client with test object
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.mcpServer).
				Build()

			// Create an in-memory database manager
			dbManager, err := database.NewManager(&database.Config{
				DatabaseType: database.DatabaseTypeSqlite,
				SqliteConfig: &database.SqliteConfig{
					DatabasePath: "file::memory:?cache=shared",
				},
			})
			require.NoError(t, err)
			defer dbManager.Close()

			err = dbManager.Initialize()
			require.NoError(t, err)

			dbClient := database.NewClient(dbManager)

			// Create reconciler
			translator := agenttranslator.NewAdkApiTranslator(
				kubeClient,
				types.NamespacedName{Namespace: "test", Name: "default-model"},
				nil,
				"",
			)
			reconciler := NewKagentReconciler(
				translator,
				kubeClient,
				dbClient,
				types.NamespacedName{Namespace: "test", Name: "default-model"},
			)

			// Call ReconcileKagentMCPServer
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: tc.mcpServer.Namespace,
					Name:      tc.mcpServer.Name,
				},
			}

			err = reconciler.ReconcileKagentMCPServer(ctx, req)

			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorText)
			} else {
				// Valid port case may still error when trying to connect to MCP server,
				// but it should not be a conversion error
				if err != nil {
					assert.NotContains(t, err.Error(), "failed to convert mcp server")
				}
			}
		})
	}
}
