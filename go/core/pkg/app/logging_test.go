package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/stretchr/testify/require"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type loggingExporter struct {
	records []sdklog.Record
}

var _ sdklog.Exporter = (*loggingExporter)(nil)

func (exporter *loggingExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}
	return nil
}

func (*loggingExporter) Shutdown(context.Context) error   { return nil }
func (*loggingExporter) ForceFlush(context.Context) error { return nil }

func TestSetupLoggerWithTelemetry(t *testing.T) {
	for _, logsEnabled := range []bool{false, true} {
		t.Run("logs="+map[bool]string{false: "disabled", true: "enabled"}[logsEnabled], func(t *testing.T) {
			exporter := &loggingExporter{}
			provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
			previous := logglobal.GetLoggerProvider()
			logglobal.SetLoggerProvider(provider)
			t.Cleanup(func() {
				require.NoError(t, provider.Shutdown(context.Background()))
				logglobal.SetLoggerProvider(previous)
			})

			require.NoError(t, SetupLoggerWithTelemetry("info", logsEnabled))
			slog.Default().InfoContext(t.Context(), "startup logger", "component", "controller")
			ctx := logging.IntoContext(t.Context(), slog.Default())
			logging.FromContext(ctx).InfoContext(ctx, "context logger", "component", "controller")
			require.NoError(t, provider.ForceFlush(t.Context()))
			if logsEnabled {
				require.Len(t, exporter.records, 2)
			} else {
				require.Empty(t, exporter.records)
			}
		})
	}
}
