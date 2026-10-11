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
	return telemetry.Config{Traces: otlp(cfg.Traces), Metrics: otlp(cfg.Metrics), Logs: otlp(cfg.Logs), ResourceAttributes: maps.Clone(cfg.ResourceAttributes)}
}

func otlp(cfg *apiv1.OTLPConfig) *telemetry.OTLPConfig {
	if cfg == nil {
		return nil
	}
	return &telemetry.OTLPConfig{Endpoint: cfg.Endpoint, Protocol: cfg.GetProtocol(), TimeoutMillis: cfg.Timeout.AsDuration().Milliseconds()}
}
