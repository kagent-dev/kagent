package telemetry

import (
	"context"
	"log/slog"

	otelslog "go.opentelemetry.io/contrib/bridges/otelslog"
	logglobal "go.opentelemetry.io/otel/log/global"
)

// loggerBridgeName is the instrumentation scope under which the controller's
// own logs are emitted by the otelslog bridge, matching the module that owns
// the controller.
const loggerBridgeName = "github.com/kagent-dev/kagent/go/core"

// ControllerLogHandler tees structured logs to the process OpenTelemetry logger
// provider while preserving the original handler's output and level filtering.
// Callers apply this only when controller log export is enabled. The global
// logger provider delegates to the SDK once Init runs, including for loggers
// created before telemetry starts.
func ControllerLogHandler(handler slog.Handler) slog.Handler {
	return &controllerLogHandler{
		Handler: handler,
		output: slog.NewMultiHandler(
			handler,
			otelslog.NewHandler(loggerBridgeName, otelslog.WithLoggerProvider(logglobal.GetLoggerProvider())),
		),
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
