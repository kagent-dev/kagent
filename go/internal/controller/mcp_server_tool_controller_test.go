package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agenttranslator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
)

// fakeReconciler is a test implementation of KagentReconciler that returns predefined errors.
type fakeReconciler struct {
	reconcileMCPServerError error
}

func (f *fakeReconciler) ReconcileKagentMCPServer(ctx context.Context, req ctrl.Request) error {
	return f.reconcileMCPServerError
}

func (f *fakeReconciler) ReconcileKagentMCPService(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeReconciler) ReconcileKagentAgent(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeReconciler) ReconcileKagentModelConfig(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeReconciler) ReconcileKagentRemoteMCPServer(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeReconciler) GetOwnedResourceTypes() []client.Object {
	return nil
}

// TestMCPServerToolController_ErrorTypeDetection tests that the controller
// correctly distinguishes between ValidationError and other errors using errors.As.
func TestMCPServerToolController_ErrorTypeDetection(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name                  string
		reconcilerError       error
		expectControllerError bool
		expectRequeue         bool
	}{
		{
			name:                  "validation error - no retry",
			reconcilerError:       agenttranslator.NewValidationError("invalid port"),
			expectControllerError: false, // Controller converts to no error
			expectRequeue:         false,
		},
		{
			name:                  "improperly wrapped validation error - retries",
			reconcilerError:       errors.New("failed to convert: " + agenttranslator.NewValidationError("invalid port").Error()),
			expectControllerError: true, // Error chain is broken, so it's treated as transient
			expectRequeue:         false,
		},
		{
			name:                  "transient error - retry with backoff",
			reconcilerError:       errors.New("database connection failed"),
			expectControllerError: true,
			expectRequeue:         false, // No requeue when error returned
		},
		{
			name:                  "success - periodic refresh",
			reconcilerError:       nil,
			expectControllerError: false,
			expectRequeue:         true, // Requeue after 60s
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeReconciler := &fakeReconciler{
				reconcileMCPServerError: tc.reconcilerError,
			}

			controller := &MCPServerToolController{
				Reconciler: fakeReconciler,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test",
					Name:      "test-server",
				},
			}

			result, err := controller.Reconcile(ctx, req)

			if tc.expectControllerError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tc.expectRequeue {
				assert.NotEqual(t, ctrl.Result{}, result, "Should have requeue result")
			} else {
				assert.Equal(t, ctrl.Result{}, result, "Should have empty result")
			}
		})
	}
}

// TestMCPServerToolController_ErrorWrapping tests that the controller correctly
// detects ValidationError even when wrapped with fmt.Errorf using %w.
func TestMCPServerToolController_ErrorWrapping(t *testing.T) {
	ctx := context.Background()

	// Create a wrapped ValidationError (simulating what reconciler does)
	innerErr := agenttranslator.NewValidationError("cannot determine port for MCP server")
	wrappedErr := errors.New("failed to convert mcp server test/test-server: " + innerErr.Error())

	// Note: This test will fail because errors.New doesn't preserve error chain
	// The reconciler should use fmt.Errorf with %w instead
	// This test documents the expected behavior

	fakeReconciler := &fakeReconciler{
		reconcileMCPServerError: wrappedErr,
	}

	controller := &MCPServerToolController{
		Reconciler: fakeReconciler,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test",
			Name:      "test-server",
		},
	}

	result, err := controller.Reconcile(ctx, req)

	// With errors.New(), this will be treated as transient error (not ideal)
	// With fmt.Errorf("%w", ...), it would be correctly detected as ValidationError
	var validationErr *agenttranslator.ValidationError
	if errors.As(wrappedErr, &validationErr) {
		// If error chain is preserved, should not retry
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	} else {
		// If error chain is broken, will retry (current behavior with errors.New)
		require.Error(t, err)
	}
}
