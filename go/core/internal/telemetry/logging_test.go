package telemetry

import (
	"context"
	"testing"

	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// TestControllerZapOpts_DisabledByDefault verifies no zap options are added when
// OTEL_LOGGING_ENABLED is off, keeping the controller logger unchanged.
func TestControllerZapOpts_DisabledByDefault(t *testing.T) {
	t.Setenv("OTEL_LOGGING_ENABLED", "false")
	if opts := ControllerZapOpts(); opts != nil {
		t.Fatalf("expected nil opts when logging disabled, got %d", len(opts))
	}
}

// TestControllerZapOpts_EnabledTeesLogger verifies that when OTEL_LOGGING_ENABLED
// is set, an otelzap bridge option is returned and the resulting logger is usable
// (the bridge tees onto the stdout core without panicking).
func TestControllerZapOpts_EnabledTeesLogger(t *testing.T) {
	t.Setenv("OTEL_LOGGING_ENABLED", "true")

	opts := ControllerZapOpts()
	if len(opts) != 1 {
		t.Fatalf("expected 1 zap opt when logging enabled, got %d", len(opts))
	}

	// Building and using the logger must not panic even though no real OTLP
	// LoggerProvider is configured (the global provider defaults to a no-op).
	logger := crzap.New(append([]crzap.Opts{crzap.UseDevMode(true)}, opts...)...)
	logger.Info("tee smoke test")
}

// TestInitLoggerProvider_DisabledNoop verifies the returned shutdown is a safe
// no-op when logging is disabled.
func TestInitLoggerProvider_DisabledNoop(t *testing.T) {
	t.Setenv("OTEL_LOGGING_ENABLED", "false")

	shutdown, err := InitLoggerProvider(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}
