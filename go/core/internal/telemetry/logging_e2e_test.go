package telemetry

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/processors/minsev"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// logsReceiver is an in-process OTLP/gRPC LogsService server. It captures every
// export request so the test can assert on what the controller actually shipped
// over the wire.
type logsReceiver struct {
	collogspb.UnimplementedLogsServiceServer
	ch chan *collogspb.ExportLogsServiceRequest
}

func (r *logsReceiver) Export(_ context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	r.ch <- req
	return &collogspb.ExportLogsServiceResponse{}, nil
}

// startLogsReceiver starts the in-process OTLP/gRPC collector on a random
// loopback port and returns its address plus the received-request channel.
func startLogsReceiver(t *testing.T) (string, <-chan *collogspb.ExportLogsServiceRequest) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	rec := &logsReceiver{ch: make(chan *collogspb.ExportLogsServiceRequest, 8)}
	srv := grpc.NewServer()
	collogspb.RegisterLogsServiceServer(srv, rec)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), rec.ch
}

// TestInitLoggerProvider_E2E_ExportsControllerLogsOverOTLP is an end-to-end test
// of the full controller log pipeline: it stands up a real OTLP/gRPC collector
// in-process, wires the actual autoexport OTLP exporter to it via the standard
// OTEL_* environment variables, builds the controller's zap logger through
// ControllerZapOpts, and asserts that a log line emitted by the controller
// arrives at the collector as an OTLP LogRecord carrying the shared resource
// (service.name=kagent-controller) and the bridge instrumentation scope.
//
// Unlike the unit tests (which use an in-memory logtest processor), this drives
// the genuine export path: zap core -> otelzap bridge -> LoggerProvider ->
// minsev processor -> batch processor -> OTLP/gRPC exporter -> collector.
func TestInitLoggerProvider_E2E_ExportsControllerLogsOverOTLP(t *testing.T) {
	addr, received := startLogsReceiver(t)

	t.Setenv("OTEL_LOGGING_ENABLED", "true")
	t.Setenv("OTEL_LOGS_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "grpc")
	// http:// scheme selects an insecure (plaintext) gRPC connection.
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://"+addr)

	ctx := context.Background()

	res, err := NewTelemetryResource(ctx, "e2e-test")
	if err != nil {
		t.Fatalf("build resource: %v", err)
	}

	shutdown, err := InitLoggerProvider(ctx, res, minsev.SeverityInfo1)
	if err != nil {
		t.Fatalf("init logger provider: %v", err)
	}

	// ControllerZapOpts binds the bridge to the global provider set above, so it
	// must be called after InitLoggerProvider.
	opts := ControllerZapOpts()
	if len(opts) != 1 {
		t.Fatalf("expected 1 zap opt when logging enabled, got %d", len(opts))
	}
	logger := crzap.New(append([]crzap.Opts{crzap.UseDevMode(true)}, opts...)...)

	logger.Info("reconciling Agent", "controller", "agent", "namespace", "kagent")

	// Shutdown flushes the batch processor synchronously, forcing the export.
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case req := <-received:
		assertLogExported(t, req)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for the controller log to reach the OTLP collector")
	}
}

// assertLogExported verifies the exported request carries the shared resource,
// the bridge scope, and the emitted log body.
func assertLogExported(t *testing.T, req *collogspb.ExportLogsServiceRequest) {
	t.Helper()

	if len(req.GetResourceLogs()) == 0 {
		t.Fatal("export request had no resource logs")
	}

	var (
		sawServiceName bool
		sawScope       bool
		sawBody        bool
		sawAttr        bool
	)
	for _, rl := range req.GetResourceLogs() {
		for _, kv := range rl.GetResource().GetAttributes() {
			if kv.GetKey() == "service.name" && kv.GetValue().GetStringValue() == ServiceName {
				sawServiceName = true
			}
		}
		for _, sl := range rl.GetScopeLogs() {
			if sl.GetScope().GetName() == loggerBridgeName {
				sawScope = true
			}
			for _, lr := range sl.GetLogRecords() {
				if strings.Contains(lr.GetBody().GetStringValue(), "reconciling Agent") {
					sawBody = true
				}
				if hasStringAttr(lr.GetAttributes(), "controller", "agent") {
					sawAttr = true
				}
			}
		}
	}

	if !sawServiceName {
		t.Errorf("exported logs missing resource attribute service.name=%q", ServiceName)
	}
	if !sawScope {
		t.Errorf("exported logs missing instrumentation scope %q", loggerBridgeName)
	}
	if !sawBody {
		t.Error("exported logs missing the emitted log body \"reconciling Agent\"")
	}
	if !sawAttr {
		t.Error("exported log record missing the controller=agent attribute")
	}
}

func hasStringAttr(attrs []*commonpb.KeyValue, key, want string) bool {
	for _, kv := range attrs {
		if kv.GetKey() == key && kv.GetValue().GetStringValue() == want {
			return true
		}
	}
	return false
}
