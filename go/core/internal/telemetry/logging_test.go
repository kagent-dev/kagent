package telemetry

import (
	"context"
	"testing"

	otelzap "go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/contrib/processors/minsev"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/logtest"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

	shutdown, err := InitLoggerProvider(context.Background(), nil, minsev.SeverityInfo1)
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

// TestMinSeverityForZap verifies the zap level -> OTel min-severity mapping.
func TestMinSeverityForZap(t *testing.T) {
	cases := []struct {
		level zapcore.LevelEnabler
		want  minsev.Severity
	}{
		{nil, minsev.SeverityInfo1},
		{zapcore.DebugLevel, minsev.SeverityDebug1},
		{zapcore.InfoLevel, minsev.SeverityInfo1},
		{zapcore.WarnLevel, minsev.SeverityWarn1},
		{zapcore.ErrorLevel, minsev.SeverityError1},
	}
	for _, tc := range cases {
		if got := MinSeverityForZap(tc.level); got != tc.want {
			t.Errorf("MinSeverityForZap(%v) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

// TestOTelZapBridge_TraceCorrelation verifies the otelzap bridge attaches the
// span (trace context) to a record only when the log call carries the request
// context as a field — documenting the correlation behaviour.
func TestOTelZapBridge_TraceCorrelation(t *testing.T) {
	recorder := logtest.NewRecorder()
	core := otelzap.NewCore("test", otelzap.WithLoggerProvider(recorder))
	logger := zap.New(core)

	tp := sdktrace.NewTracerProvider()
	spanCtx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()
	wantTraceID := span.SpanContext().TraceID()

	// With the context passed as a field: record carries the active span.
	logger.Info("with ctx", zap.Any("ctx", spanCtx))
	// Without it (the generic controller-runtime path): no span linked.
	logger.Info("without ctx")

	records := flatten(recorder.Result())
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if got := trace.SpanContextFromContext(records[0].Context).TraceID(); got != wantTraceID {
		t.Errorf("record with ctx: trace id = %v, want %v", got, wantTraceID)
	}
	if trace.SpanContextFromContext(records[1].Context).TraceID().IsValid() {
		t.Error("record without ctx should not carry a trace id")
	}
}

// TestMinSeverityProcessor_GatesBelowThreshold verifies records below the
// configured minimum severity are dropped by the minsev processor.
func TestMinSeverityProcessor_GatesBelowThreshold(t *testing.T) {
	capture := &sliceProcessor{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(minsev.NewLogProcessor(capture, minsev.SeverityInfo1)))
	logger := lp.Logger("test")

	emit := func(sev otellog.Severity) {
		var r otellog.Record
		r.SetSeverity(sev)
		logger.Emit(context.Background(), r)
	}
	emit(otellog.SeverityDebug) // below Info -> dropped
	emit(otellog.SeverityInfo)  // kept
	emit(otellog.SeverityError) // above Info -> kept

	if len(capture.records) != 2 {
		t.Fatalf("expected 2 records at/above Info, got %d", len(capture.records))
	}
	for _, r := range capture.records {
		if r.Severity() < otellog.SeverityInfo {
			t.Errorf("record below Info leaked through: %v", r.Severity())
		}
	}
}

func flatten(rec logtest.Recording) []logtest.Record {
	var out []logtest.Record
	for _, rs := range rec {
		out = append(out, rs...)
	}
	return out
}

// sliceProcessor is a minimal sdklog.Processor that captures emitted records.
type sliceProcessor struct{ records []sdklog.Record }

func (p *sliceProcessor) OnEmit(_ context.Context, r *sdklog.Record) error {
	p.records = append(p.records, *r)
	return nil
}
func (p *sliceProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }
func (p *sliceProcessor) Shutdown(context.Context) error                         { return nil }
func (p *sliceProcessor) ForceFlush(context.Context) error                       { return nil }
