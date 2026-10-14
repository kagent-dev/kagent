package config

import (
	"cmp"
	"maps"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// RuntimeTelemetry projects schema settings without reading the environment.
// Each receiving process applies its own OTEL overrides at startup.
func RuntimeTelemetry(cfg *apiv1.TelemetryConfig) tracing.RuntimeTelemetry {
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
func Telemetry(cfg *apiv1.TelemetryConfig) telemetry.Config {
	if cfg == nil {
		return telemetry.Config{}
	}
	return telemetry.Config{Traces: otlp(cfg.GetTraces()), Metrics: otlp(cfg.GetMetrics()), Logs: otlp(cfg.GetLogs()), ResourceAttributes: maps.Clone(cfg.GetResourceAttributes())}
}

func otlp(cfg *apiv1.OTLPConfig) *telemetry.OTLPConfig {
	if cfg == nil {
		return nil
	}
	return &telemetry.OTLPConfig{Endpoint: cfg.GetEndpoint(), Protocol: cmp.Or(cfg.GetProtocol(), "grpc"), TimeoutMillis: cfg.GetTimeout().AsDuration().Milliseconds()}
}
