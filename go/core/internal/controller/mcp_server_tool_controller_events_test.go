package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kmcp/api/v1alpha1"

	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

// TestMCPServerToolController_RecordsEvents verifies the controller emits a
// Kubernetes Event with the expected type/reason for each reconcile outcome.
func TestMCPServerToolController_RecordsEvents(t *testing.T) {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	// Generation != ObservedGeneration => a pending spec change, so the success
	// Event is emitted (see the no-op case below for the periodic-refresh path).
	server := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "test-server", Generation: 1},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "test-server"}}

	testCases := []struct {
		name          string
		reconcileErr  error
		wantEventType string
		wantReason    string
	}{
		{"success emits ToolsDiscovered", nil, "Normal", "ToolsDiscovered"},
		{"validation error emits ValidationFailed", agenttranslator.NewValidationError("invalid port"), "Warning", "ValidationFailed"},
		{"transient error emits ReconcileFailed", errors.New("database connection failed"), "Warning", "ReconcileFailed"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server).Build()
			recorder := events.NewFakeRecorder(1)

			controller := &MCPServerToolController{
				Reconciler: &fakeReconciler{reconcileMCPServerError: tc.reconcileErr},
				Client:     cl,
				Recorder:   recorder,
			}

			if _, err := controller.Reconcile(ctx, req); tc.wantEventType == "Warning" && tc.wantReason == "ReconcileFailed" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			select {
			case got := <-recorder.Events:
				assert.Contains(t, got, tc.wantEventType)
				assert.Contains(t, got, tc.wantReason)
			default:
				t.Fatalf("expected an event with reason %q, got none", tc.wantReason)
			}
		})
	}
}

// TestMCPServerToolController_SkipsSuccessEventOnPeriodicRefresh verifies that a
// successful reconcile with no pending spec change (generation ==
// observedGeneration, i.e. a periodic 60s refresh) does NOT emit a success
// Event, so the Events feed is not flooded.
func TestMCPServerToolController_SkipsSuccessEventOnPeriodicRefresh(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	server := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "test-server", Generation: 3},
		Status:     v1alpha1.MCPServerStatus{ObservedGeneration: 3},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server).Build()
	recorder := events.NewFakeRecorder(1)
	controller := &MCPServerToolController{Reconciler: &fakeReconciler{}, Client: cl, Recorder: recorder}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "test-server"}}

	if _, err := controller.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case got := <-recorder.Events:
		t.Fatalf("expected no success Event on a periodic refresh, got %q", got)
	default:
	}
}

// TestMCPServerToolController_NoRecorderNoPanic verifies event recording is a
// safe no-op when no Recorder/Client is wired (the default wiring).
func TestMCPServerToolController_NoRecorderNoPanic(t *testing.T) {
	controller := &MCPServerToolController{Reconciler: &fakeReconciler{}}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "test-server"}}
	// Must not panic despite nil Recorder/Client.
	if _, err := controller.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
