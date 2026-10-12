package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type controllerLogExporter struct {
	records []sdklog.Record
}

var _ sdklog.Exporter = (*controllerLogExporter)(nil)

func (exporter *controllerLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}
	return nil
}

func (*controllerLogExporter) Shutdown(context.Context) error   { return nil }
func (*controllerLogExporter) ForceFlush(context.Context) error { return nil }

func TestControllerLogHandler(t *testing.T) {
	exporter := &controllerLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	previous := logglobal.GetLoggerProvider()
	logglobal.SetLoggerProvider(provider)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
		logglobal.SetLoggerProvider(previous)
	})

	var output bytes.Buffer
	base, err := logging.New(&output, "warn")
	require.NoError(t, err)

	logger := slog.New(ControllerLogHandler(base.Handler())).
		With("component", "controller").
		WithGroup("reconcile").
		With("name", "tools")
	logger.InfoContext(t.Context(), "filtered")
	logger.WarnContext(t.Context(), "discovery failed", "attempt", 2)

	var record struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		Component string `json:"component"`
		Reconcile struct {
			Name    string `json:"name"`
			Attempt int    `json:"attempt"`
		} `json:"reconcile"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	require.Equal(t, "WARN", record.Level)
	require.Equal(t, "discovery failed", record.Message)
	require.Equal(t, "controller", record.Component)
	require.Equal(t, "tools", record.Reconcile.Name)
	require.Equal(t, 2, record.Reconcile.Attempt)
	require.Len(t, exporter.records, 1)
	require.Equal(t, "discovery failed", exporter.records[0].Body().AsString())
	require.Equal(t, otellog.SeverityWarn, exporter.records[0].Severity())
}
