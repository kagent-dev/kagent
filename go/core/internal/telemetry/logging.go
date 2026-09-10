package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	otelzap "go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// loggerBridgeName is the instrumentation scope under which the controller's
// own logs are emitted by the otelzap bridge, matching the module that owns
// the controller.
const loggerBridgeName = "github.com/kagent-dev/kagent/go/core"

// InitLoggerProvider configures an OTLP LoggerProvider and registers it as the
// global OTel logger provider. The exporter type and endpoint are read from the
// standard OTEL environment variables via autoexport, mirroring
// InitTracerProvider, so log and trace pipelines share the same OTLP config.
// The returned shutdown function must be called on process exit to flush
// in-flight log records. When OTEL_LOGGING_ENABLED is unset (the default) the
// pipeline is not created and a no-op shutdown is returned.
func InitLoggerProvider(ctx context.Context, serviceVersion string) (func(context.Context) error, error) {
	if !env.OtelLoggingEnabled.Get() {
		return func(context.Context) error { return nil }, nil
	}

	res, err := newTelemetryResource(ctx, serviceVersion)
	if err != nil {
		return nil, fmt.Errorf("create logging resource: %w", err)
	}

	exporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create log exporter: %w", err)
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)

	logglobal.SetLoggerProvider(lp)

	return lp.Shutdown, nil
}

// ControllerLogHandler adds an otelzap bridge while preserving the original
// handler's output and level filtering. The global logger provider delegates
// to the SDK once InitLoggerProvider runs, including for early startup loggers.
func ControllerLogHandler(handler slog.Handler) slog.Handler {
	if !env.OtelLoggingEnabled.Get() {
		return handler
	}
	bridgeCore := otelzap.NewCore(loggerBridgeName,
		otelzap.WithLoggerProvider(logglobal.GetLoggerProvider()),
	)
	return &controllerLogHandler{
		Handler: handler,
		output:  slog.NewMultiHandler(handler, logr.ToSlogHandler(zapr.NewLogger(zap.New(bridgeCore)))),
	}
}

type controllerLogHandler struct {
	slog.Handler
	output slog.Handler
}

var _ slog.Handler = (*controllerLogHandler)(nil)

func (handler *controllerLogHandler) Handle(ctx context.Context, record slog.Record) error {
	return handler.output.Handle(ctx, record)
}

func (handler *controllerLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &controllerLogHandler{Handler: handler.Handler.WithAttrs(attrs), output: handler.output.WithAttrs(attrs)}
}

func (handler *controllerLogHandler) WithGroup(name string) slog.Handler {
	return &controllerLogHandler{Handler: handler.Handler.WithGroup(name), output: handler.output.WithGroup(name)}
}
