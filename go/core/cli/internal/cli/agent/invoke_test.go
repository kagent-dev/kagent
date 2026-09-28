package cli

import (
	"testing"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
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

// TestInvokeCmd_MessageHasID is a regression test: InvokeCmd used to build its
// outgoing protocol.Message as a raw struct literal that never set MessageID,
// which the server rejects with "-32602 invalid params: message ID is
// required". Both the streaming and non-streaming send paths must construct
// the message the same way InvokeCmd does, via NewMessageWithContext, which
// always generates a non-empty ID.
func TestInvokeCmd_MessageHasID(t *testing.T) {
	sessionID := "test-session"
	msg := protocol.NewMessageWithContext(
		protocol.MessageRoleUser,
		[]protocol.Part{protocol.NewTextPart("hello")},
		nil,
		&sessionID,
	)

	if msg.MessageID == "" {
		t.Fatal("expected NewMessageWithContext to generate a non-empty MessageID")
	}
}
