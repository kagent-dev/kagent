package reconciler

import (
	"context"
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
	"github.com/kagent-dev/kmcp/api/v1alpha1"
)

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
				[]string{}, // No namespace restrictions for tests
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
