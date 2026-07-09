package telemetry

import (
	"context"
	"fmt"

	otelzap "go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/processors/minsev"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

const loggerBridgeName = "github.com/kagent-dev/kagent/go/core"

// InitLoggerProvider configures an OTLP LoggerProvider and registers it as the
// global OTel logger provider, so the controller's own logs can be shipped to
// an OTLP backend (in addition to stdout) via ControllerZapOpts. The exporter
// type and endpoint are read from the standard OTEL environment variables via
// autoexport, mirroring InitTracerProvider, and the shared resource is passed
// in so logs and traces report identical resource attributes.
//
// Records are filtered by a min-severity processor (minsev) set to minSeverity
// — typically the controller's stdout log level — so enabling OTLP export does
// not ship info/debug records when the operator is running at a higher level.
//
// The returned shutdown function must be called on process exit to flush
// in-flight log records. When OTEL_LOGGING_ENABLED is unset the pipeline is not
// created and a no-op shutdown is returned (default-OFF).
func InitLoggerProvider(ctx context.Context, res *resource.Resource, minSeverity minsev.Severity) (func(context.Context) error, error) {
	if !env.OtelLoggingEnabled.Get() {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create log exporter: %w", err)
	}

	// Severity filtering belongs in a processor, not the exporter/bridge, so the
	// pipeline honours the configured minimum level.
	processor := minsev.NewLogProcessor(sdklog.NewBatchProcessor(exporter), minSeverity)

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(processor),
		sdklog.WithResource(res),
	)

	logglobal.SetLoggerProvider(lp)

	return lp.Shutdown, nil
}

// ControllerZapOpts returns controller-runtime zap options. When
// OTEL_LOGGING_ENABLED is set it additively tees the controller's stdout zap
// core with an otelzap bridge core, exporting the controller's logs through the
// global OTLP LoggerProvider while preserving stdout logging. When disabled it
// returns no options, leaving the logger byte-identical to upstream.
//
// InitLoggerProvider must be called first so the bridge core binds to the
// configured global LoggerProvider.
//
// Note on trace correlation: otelzap only attaches trace_id/span_id when a log
// call carries a context.Context as a zap field. controller-runtime's
// logr->zapr path does not thread the reconcile context into fields, so records
// emitted via log.FromContext(ctx) are not automatically span-linked. Callers
// that pass the request context as a field do get correlated records (see
// tests). Automatic correlation for all controller logs would need a separate
// mechanism and is out of scope here.
func ControllerZapOpts() []crzap.Opts {
	if !env.OtelLoggingEnabled.Get() {
		return nil
	}
	bridgeCore := otelzap.NewCore(loggerBridgeName,
		otelzap.WithLoggerProvider(logglobal.GetLoggerProvider()),
	)
	return []crzap.Opts{
		crzap.RawZapOpts(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, bridgeCore)
		})),
	}
}

// MinSeverityForZap maps a zap level enabler to the equivalent OTel log
// min-severity, so the OTLP log pipeline ships the same levels as stdout.
// A nil enabler (no explicit level) defaults to Info.
func MinSeverityForZap(le zapcore.LevelEnabler) minsev.Severity {
	switch {
	case le == nil:
		return minsev.SeverityInfo1
	case le.Enabled(zapcore.DebugLevel):
		return minsev.SeverityDebug1
	case le.Enabled(zapcore.InfoLevel):
		return minsev.SeverityInfo1
	case le.Enabled(zapcore.WarnLevel):
		return minsev.SeverityWarn1
	default:
		return minsev.SeverityError1
	}
}
