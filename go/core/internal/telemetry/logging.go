package telemetry

import (
	"context"
	"fmt"

	otelzap "go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

const loggerBridgeName = "github.com/kagent-dev/kagent/go/core"

// InitLoggerProvider configures an OTLP LoggerProvider and registers it as the
// global OTel logger provider. The exporter type and endpoint are read from the
// standard OTEL environment variables via autoexport, mirroring
// InitTracerProvider. The returned shutdown function must be called on process
// exit to flush in-flight log records. When OTEL_LOGGING_ENABLED is unset the
// pipeline is not created and a no-op shutdown is returned (default-OFF).
func InitLoggerProvider(ctx context.Context, serviceVersion string) (func(context.Context) error, error) {
	if !env.OtelLoggingEnabled.Get() {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create log exporter: %w", err)
	}

	res, err := newTelemetryResource(ctx, serviceVersion)
	if err != nil {
		return nil, err
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)

	logglobal.SetLoggerProvider(lp)

	return lp.Shutdown, nil
}

// ControllerZapOpts returns controller-runtime zap options. When
// OTEL_LOGGING_ENABLED is set it additively tees the controller's stdout zap
// core with an otelzap bridge core, routing the controller's own logs through
// the global OTLP LoggerProvider while preserving stdout logging. When disabled
// it returns no options, leaving the logger byte-identical to upstream.
//
// InitLoggerProvider must be called first so the bridge core binds to the
// configured global LoggerProvider.
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
