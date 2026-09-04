package logging

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/go-logr/logr"
)

// New returns a JSON logger at the requested level.
func New(w io.Writer, level string) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return nil, err
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parsed})), nil
}

// NewFromEnv returns a JSON logger using LOG_LEVEL, defaulting to info.
func NewFromEnv(w io.Writer) (*slog.Logger, error) {
	return New(w, cmp.Or(os.Getenv("LOG_LEVEL"), "info"))
}

// IntoContext stores logger in the logr context slot used by controller-runtime.
func IntoContext(ctx context.Context, logger *slog.Logger) context.Context {
	return logr.NewContext(ctx, logr.FromSlogHandler(logger.Handler()))
}

// FromContext returns the controller-runtime logger carried by ctx, or the
// process default when ctx has no logger.
func FromContext(ctx context.Context) *slog.Logger {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return slog.Default()
	}
	return slog.New(logr.ToSlogHandler(logger))
}

// FromLogr adapts a logger supplied by controller-runtime.
func FromLogr(logger logr.Logger) *slog.Logger {
	return slog.New(logr.ToSlogHandler(logger))
}

// AsLogr adapts logger for dependencies which still require logr.
func AsLogr(logger *slog.Logger) logr.Logger {
	return logr.FromSlogHandler(logger.Handler())
}
