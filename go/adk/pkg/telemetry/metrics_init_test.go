package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestMetricEndpointPaths(t *testing.T) {
	for _, test := range []struct{ name, endpointPath, signalPath, wantPath string }{
		{name: "generic endpoint", wantPath: "/v1/metrics"},
		{name: "generic prefix", endpointPath: "/collector", wantPath: "/collector/v1/metrics"},
		{name: "signal override", signalPath: "/custom-metrics", wantPath: "/custom-metrics"},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := make(chan string, 4)
			collector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				paths <- request.URL.Path
				writer.Header().Set("Content-Type", "application/x-protobuf")
			}))
			t.Cleanup(collector.Close)
			t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/protobuf")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL+test.endpointPath)
			t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
			if test.signalPath != "" {
				t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", collector.URL+test.signalPath)
			}
			provider, err := newMeterProvider(t.Context(), resource.Empty())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			histogram, err := provider.Meter("test").Int64Histogram("tokens")
			require.NoError(t, err)
			histogram.Record(t.Context(), 10)
			require.NoError(t, provider.ForceFlush(t.Context()))
			select {
			case path := <-paths:
				require.Equal(t, test.wantPath, path)
			default:
				t.Fatal("metrics were not exported")
			}
		})
	}
}

func TestInitSignalGates(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(collector.Close)
	for _, test := range []struct {
		name, traces, logs, metrics, protocol string
		wantErr                               bool
	}{
		{name: "default off", protocol: "invalid"},
		{name: "metrics only", metrics: "true"},
		{name: "traces only", traces: "true"},
		{name: "logs only", logs: "true"},
		{name: "all signals", traces: "true", logs: "true", metrics: "true"},
		{name: "metric failure preserves globals", traces: "true", logs: "true", metrics: "true", protocol: "invalid", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACING_ENABLED", test.traces)
			t.Setenv("OTEL_LOGGING_ENABLED", test.logs)
			t.Setenv("OTEL_METRICS_ENABLED", test.metrics)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
			t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", test.protocol)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
			previousTrace := otel.GetTracerProvider()
			previousLog := logglobal.GetLoggerProvider()
			previousMeter := otel.GetMeterProvider()
			previousPropagator := otel.GetTextMapPropagator()
			previousHistogram := tokenUsageHistogram.Load()
			t.Cleanup(func() {
				otel.SetTracerProvider(previousTrace)
				logglobal.SetLoggerProvider(previousLog)
				otel.SetMeterProvider(previousMeter)
				otel.SetTextMapPropagator(previousPropagator)
				tokenUsageHistogram.Store(previousHistogram)
			})
			shutdown, enabled, err := Init(t.Context(), "test", "test")
			if test.wantErr {
				require.Error(t, err)
				require.Nil(t, shutdown)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.traces == "true" || test.logs == "true" || test.metrics == "true", enabled)
				require.NoError(t, shutdown(t.Context()))
			}
			if test.traces != "true" || test.wantErr {
				require.Same(t, previousTrace, otel.GetTracerProvider())
			}
			if test.logs != "true" || test.wantErr {
				require.Same(t, previousLog, logglobal.GetLoggerProvider())
			}
			if test.metrics != "true" || test.wantErr {
				require.Same(t, previousMeter, otel.GetMeterProvider())
			}
		})
	}
}

func TestTokenRecorderConcurrentInitialization(t *testing.T) {
	previous := tokenUsageHistogram.Load()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	t.Cleanup(func() {
		tokenUsageHistogram.Store(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 100 {
				initTokenUsageRecorder(provider)
				RecordTokenUsage(t.Context(), TokenUsage{InputTokens: 1})
			}
		})
	}
	workers.Wait()
}

func TestShutdownContinuesAfterTraceError(t *testing.T) {
	traceErr := errors.New("trace shutdown failed")
	metricErr := errors.New("metric shutdown failed")
	var stopped []string
	err := shutdownProviders(t.Context(), []func(context.Context) error{
		func(context.Context) error { stopped = append(stopped, "traces"); return traceErr },
		func(context.Context) error { stopped = append(stopped, "logs"); return nil },
		func(context.Context) error { stopped = append(stopped, "metrics"); return metricErr },
	})
	require.ErrorIs(t, err, traceErr)
	require.ErrorIs(t, err, metricErr)
	require.Equal(t, []string{"traces", "logs", "metrics"}, stopped)
}
