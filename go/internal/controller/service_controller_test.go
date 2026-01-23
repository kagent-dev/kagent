package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

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

// TestServiceController_ValidationError tests that the controller does not retry
// when the reconciler returns a ValidationError for a Service with invalid MCP configuration.
func TestServiceController_ValidationError(t *testing.T) {
	ctx := context.Background()

	// Create fake reconciler that returns ValidationError
	fakeReconciler := &fakeServiceReconciler{
		reconcileServiceError: agenttranslator.NewValidationError("no port found for service test-service"),
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

	// Should return empty result with no error (no retry)
	require.NoError(t, err, "ValidationError should not be returned as error to avoid retry")
	assert.Equal(t, ctrl.Result{}, result, "Result should be empty for validation errors")
}

// TestServiceController_TransientError tests that the controller returns
// an error for transient errors to trigger exponential backoff.
func TestServiceController_TransientError(t *testing.T) {
	ctx := context.Background()

	// Create fake reconciler that returns a transient error
	fakeReconciler := &fakeServiceReconciler{
		reconcileServiceError: errors.New("failed to connect to MCP server"),
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

	// Should return error to trigger retry with exponential backoff
	require.Error(t, err, "Transient error should be returned to trigger retry")
	assert.Equal(t, ctrl.Result{}, result, "Result should be empty when returning error")
	assert.Contains(t, err.Error(), "failed to connect to MCP server")
}

// TestServiceController_Success tests that the controller handles
// successful reconciliation.
func TestServiceController_Success(t *testing.T) {
	ctx := context.Background()

	// Create fake reconciler that returns no error (success)
	fakeReconciler := &fakeServiceReconciler{
		reconcileServiceError: nil,
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

	// Should return no error and empty result (no periodic refresh for services)
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result, "Result should be empty for successful service reconciliation")
}

// TestServiceController_InvalidPortAnnotation tests validation of
// port annotation parsing.
func TestServiceController_InvalidPortAnnotation(t *testing.T) {
	ctx := context.Background()

	// Create fake reconciler that returns ValidationError for invalid port annotation
	fakeReconciler := &fakeServiceReconciler{
		reconcileServiceError: agenttranslator.NewValidationError("port in annotation kagent.dev/mcp-service-port is not a valid integer"),
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

	// Should return empty result with no error (no retry for validation errors)
	require.NoError(t, err, "ValidationError should not be returned as error to avoid retry")
	assert.Equal(t, ctrl.Result{}, result, "Result should be empty for validation errors")
}

// TestServiceController_ErrorTypeDetection tests that the controller
// correctly distinguishes between ValidationError and other errors.
func TestServiceController_ErrorTypeDetection(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name              string
		reconcilerError   error
		expectControllerError bool
	}{
		{
			name:              "no ports - validation error",
			reconcilerError:   agenttranslator.NewValidationError("no port found"),
			expectControllerError: false,
		},
		{
			name:              "invalid port annotation - validation error",
			reconcilerError:   agenttranslator.NewValidationError("port is not a valid integer"),
			expectControllerError: false,
		},
		{
			name:              "network error - transient",
			reconcilerError:   errors.New("connection timeout"),
			expectControllerError: true,
		},
		{
			name:              "database error - transient",
			reconcilerError:   errors.New("database unavailable"),
			expectControllerError: true,
		},
		{
			name:              "success - no error",
			reconcilerError:   nil,
			expectControllerError: false,
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

			// Services always return empty result
			assert.Equal(t, ctrl.Result{}, result)
		})
	}
}

// TestServiceController_MultipleValidationErrors tests handling of different
// validation error scenarios.
func TestServiceController_MultipleValidationErrors(t *testing.T) {
	ctx := context.Background()

	validationErrors := []error{
		agenttranslator.NewValidationError("no port found for service with protocol streamable-http"),
		agenttranslator.NewValidationError("port in annotation is not a valid integer: invalid syntax"),
		agenttranslator.NewValidationError("service has no ports and no port annotation"),
	}

	for _, validationErr := range validationErrors {
		fakeReconciler := &fakeServiceReconciler{
			reconcileServiceError: validationErr,
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

		// All validation errors should result in no retry
		require.NoError(t, err, "Validation error should not be returned: %v", validationErr)
		assert.Equal(t, ctrl.Result{}, result, "Result should be empty for validation error: %v", validationErr)
	}
}
