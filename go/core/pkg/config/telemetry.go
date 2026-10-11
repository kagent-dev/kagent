package config

import (
	"maps"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
)

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
	return &telemetry.OTLPConfig{Endpoint: cfg.GetEndpoint(), Protocol: cfg.GetProtocol(), TimeoutMillis: cfg.GetTimeout().AsDuration().Milliseconds()}
}
