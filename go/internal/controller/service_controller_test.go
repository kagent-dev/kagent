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

// fakeServiceReconciler is a test implementation of KagentReconciler for Service tests.
type fakeServiceReconciler struct {
	reconcileServiceError error
}

func (f *fakeServiceReconciler) ReconcileKagentMCPServer(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeServiceReconciler) ReconcileKagentMCPService(ctx context.Context, req ctrl.Request) error {
	return f.reconcileServiceError
}

func (f *fakeServiceReconciler) ReconcileKagentAgent(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeServiceReconciler) ReconcileKagentModelConfig(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeServiceReconciler) ReconcileKagentRemoteMCPServer(ctx context.Context, req ctrl.Request) error {
	return nil
}

func (f *fakeServiceReconciler) GetOwnedResourceTypes() []client.Object {
	return nil
}

// TestServiceController_ErrorTypeDetection tests that the controller
// correctly distinguishes between ValidationError and other errors.
func TestServiceController_ErrorTypeDetection(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name                  string
		reconcilerError       error
		expectControllerError bool
		expectRequeue         bool
	}{
		{
			name:                  "no ports - validation error",
			reconcilerError:       agenttranslator.NewValidationError("no port found"),
			expectControllerError: false,
			expectRequeue:         false,
		},
		{
			name:                  "invalid port annotation - validation error",
			reconcilerError:       agenttranslator.NewValidationError("port is not a valid integer"),
			expectControllerError: false,
			expectRequeue:         false,
		},
		{
			name:                  "network error - transient",
			reconcilerError:       errors.New("connection timeout"),
			expectControllerError: true,
			expectRequeue:         false,
		},
		{
			name:                  "database error - transient",
			reconcilerError:       errors.New("database unavailable"),
			expectControllerError: true,
			expectRequeue:         false,
		},
		{
			name:                  "success - periodic refresh",
			reconcilerError:       nil,
			expectControllerError: false,
			expectRequeue:         true, // Services now requeue after 60s like MCPServers
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeReconciler := &fakeServiceReconciler{
				reconcileServiceError: tc.reconcilerError,
			}

			controller := &ServiceController{
				Reconciler: fakeReconciler,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test",
					Name:      "test-service",
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
				assert.Equal(t, float64(60), result.RequeueAfter.Seconds(), "Should requeue after 60 seconds")
			} else {
				assert.Equal(t, ctrl.Result{}, result, "Should have empty result")
			}
		})
	}
}
