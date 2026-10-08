package e2e_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	ctrl "sigs.k8s.io/controller-runtime"
)

var suiteTraceReceiver *otlpTraceReceiver

func TestMain(m *testing.M) {
	ctrl.SetLogger(logging.AsLogr(slog.Default()))
	address := kagentenv.E2EOTLPListenAddress.Get()
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
	if target := kagentenv.E2ELiveCheckEndpoint.Get(); target != "" {
		if err := receiver.replay(context.Background(), target); err != nil {
			fmt.Fprintf(os.Stderr, "replay OTLP traces to %s: %v\n", target, err)
		}
	}
	if err := stop(); err != nil {
		fmt.Fprintf(os.Stderr, "serve OTLP traces: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
