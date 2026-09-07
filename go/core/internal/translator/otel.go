package translator

import (
	"os"

	corev1 "k8s.io/api/core/v1"
)

// These are the tracing settings read by the runtime; trace-specific values
// take precedence over their generic OTLP counterparts.
// Keep this list explicit: headers may contain credentials and resource
// attributes belong to the controller rather than its agent runtimes.
var otelEnvNames = []string{
	"OTEL_TRACING_ENABLED",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
}

// OtelEnvFromProcess returns the controller's supported tracing configuration
// for the agent runtime.
func OtelEnvFromProcess() []corev1.EnvVar {
	envVars := make([]corev1.EnvVar, 0, len(otelEnvNames))
	for _, name := range otelEnvNames {
		if value, found := os.LookupEnv(name); found {
			envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
		}
	}
	return envVars
}
