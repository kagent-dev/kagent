package config

import (
	"cmp"
	"maps"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// RuntimeTelemetry projects schema settings without reading the environment.
// Each receiving process applies its own OTEL overrides at startup.
func RuntimeTelemetry(cfg *configv1.TelemetryConfig) tracing.RuntimeTelemetry {
	if cfg == nil {
		return tracing.RuntimeTelemetry{}
	}
	exporters := Telemetry(cfg)
	return tracing.RuntimeTelemetry{
		Exporters:           &exporters,
		CaptureContent:      cfg.GetCaptureContent(),
		CaptureRawAPIBodies: cfg.GetCaptureRawApiBodies(),
		OpenAINativeTracing: cfg.GetOpenaiNativeTracing(),
		MaxCaptureBytes:     int(cfg.GetMaxCaptureBytes()),
	}
}

// Telemetry converts the validated file schema to the shared SDK settings.
func Telemetry(cfg *configv1.TelemetryConfig) telemetry.Config {
	if cfg == nil {
		return telemetry.Config{}
	}
	return telemetry.Config{Traces: otlp(cfg.GetTraces()), Metrics: otlp(cfg.GetMetrics()), Logs: otlp(cfg.GetLogs()), ResourceAttributes: maps.Clone(cfg.GetResourceAttributes())}
}

func otlp(cfg *configv1.OTLPConfig) *telemetry.OTLPConfig {
	if cfg == nil {
		return nil
	}
	return &telemetry.OTLPConfig{Endpoint: cfg.GetEndpoint(), Protocol: cmp.Or(cfg.GetProtocol(), "grpc"), TimeoutMillis: cfg.GetTimeout().AsDuration().Milliseconds()}
}
