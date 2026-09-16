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

type logExporter struct {
	records []sdklog.Record
}

var _ sdklog.Exporter = (*logExporter)(nil)

func (exporter *logExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}
	return nil
}

func (*logExporter) Shutdown(context.Context) error   { return nil }
func (*logExporter) ForceFlush(context.Context) error { return nil }

func TestControllerLogHandler(t *testing.T) {
	for _, enabled := range []string{"", "false", "true"} {
		t.Run("enabled="+enabled, func(t *testing.T) {
			t.Setenv("OTEL_LOGGING_ENABLED", enabled)
			exporter := &logExporter{}
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
			handler := ControllerLogHandler(base.Handler())
			if enabled != "true" {
				require.Same(t, base.Handler(), handler)
			}
			logger := slog.New(handler).With("component", "controller").WithGroup("reconcile").With("name", "tools")
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
			if enabled == "true" {
				require.Len(t, exporter.records, 1)
				require.Equal(t, "discovery failed", exporter.records[0].Body().AsString())
				require.Equal(t, otellog.SeverityWarn, exporter.records[0].Severity())
			} else {
				require.Empty(t, exporter.records)
			}
		})
	}
}

func TestInitLoggerProvider(t *testing.T) {
	for _, test := range []struct {
		name, gate, exporter, resource string
		wantErr                        bool
	}{
		{name: "default off ignores broken config", exporter: "invalid", resource: "invalid"},
		{name: "explicit off ignores broken config", gate: "false", exporter: "invalid"},
		{name: "enabled", gate: "true", exporter: "none"},
		{name: "exporter failure", gate: "true", exporter: "invalid", wantErr: true},
		{name: "resource failure", gate: "true", exporter: "none", resource: "invalid", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_LOGGING_ENABLED", test.gate)
			t.Setenv("OTEL_LOGS_EXPORTER", test.exporter)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", test.resource)
			previous := logglobal.GetLoggerProvider()
			t.Cleanup(func() { logglobal.SetLoggerProvider(previous) })
			shutdown, err := InitLoggerProvider(t.Context(), "test")
			if test.wantErr {
				require.Error(t, err)
				require.Nil(t, shutdown)
			} else {
				require.NoError(t, err)
				require.NoError(t, shutdown(t.Context()))
			}
			if test.gate != "true" || test.wantErr {
				require.Same(t, previous, logglobal.GetLoggerProvider())
			} else {
				require.IsType(t, &sdklog.LoggerProvider{}, logglobal.GetLoggerProvider())
			}
		})
	}
}
