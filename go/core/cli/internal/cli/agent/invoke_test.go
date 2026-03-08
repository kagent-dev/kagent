package cli

import (
	"testing"
)

// Note: Most InvokeCmd tests require K8s port-forwarding mock which is complex.
// Testing InvokeCmd with URLOverride still attempts port-forward first.
// Integration tests cover the full invoke workflow.

func TestInvokeCmd_ServerError(t *testing.T) {
	// This behavior is exercised in integration tests, which can safely depend on
	// Kubernetes port-forwarding and external tooling like kubectl.
	// Invoking InvokeCmd here would trigger CheckServerConnection and may start a
	// real kubectl port-forward, which is not appropriate for unit tests.
	t.Skip("Skipping InvokeCmd server error test in unit suite; covered by integration tests without requiring kubectl/port-forwarding")
}
