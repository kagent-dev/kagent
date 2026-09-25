package e2e_test

import (
	"fmt"
	"net"
	"os"
	"testing"
)

var suiteTraceReceiver *otlpTraceReceiver

func TestMain(m *testing.M) {
	address := os.Getenv("KAGENT_E2E_OTLP_LISTEN_ADDRESS")
	if address == "" {
		os.Exit(m.Run())
	}
	// Tracing is enabled on every CI runtime, so the receiver must outlive
	// individual tests, including the sequential controller restart test.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen for OTLP traces on %s: %v\n", address, err)
		os.Exit(1)
	}
	receiver, stop := serveOTLPTraceReceiver(listener)
	suiteTraceReceiver = receiver
	code := m.Run()
	if err := stop(); err != nil {
		fmt.Fprintf(os.Stderr, "serve OTLP traces: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
