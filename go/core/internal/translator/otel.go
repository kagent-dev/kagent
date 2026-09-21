package translator

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	otelTracingEnabled             = "OTEL_TRACING_ENABLED"
	otelLoggingEnabled             = "OTEL_LOGGING_ENABLED"
	otelExporterOTLPEndpoint       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOTLPTracesEndpoint = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	otelExporterOTLPLogsEndpoint   = "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"
	otelExporterOTLPProtocol       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	otelExporterOTLPTracesProtocol = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
	otelExporterOTLPLogsProtocol   = "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"
	otelCaptureSensitiveContent    = "KAGENT_OTEL_CAPTURE_SENSITIVE_CONTENT"
	otelCaptureRawAPIBodies        = "KAGENT_OTEL_CAPTURE_RAW_API_BODIES"
	otelSpanAttributeValueLimit    = "OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT"
	defaultOTLPProtocol            = "grpc"
)

// Extra OTEL_* settings TelemetryConfig does not compile. Endpoints, protocols,
// and enable flags are resolved into signal-specific env by TelemetryConfig.
// Headers may contain credentials and resource attributes belong to the
// controller rather than its agent runtimes.
var otelEnvNames = []string{
	otelSpanAttributeValueLimit,
}

// TelemetryConfig is the controller-owned telemetry configuration compiled
// into runtime revisions. Invalid signals are disabled before compilation.
type TelemetryConfig struct {
	Traces                  SignalConfig
	Logs                    SignalConfig
	CaptureSensitiveContent bool
	CaptureRawAPIBodies     bool
}

// SignalConfig is the resolved export configuration for one telemetry signal.
type SignalConfig struct {
	Enabled  bool
	Endpoint string
	Protocol string
	Hostname string
}

// TelemetryConfigFromProcess resolves the telemetry settings inherited by
// agent runtimes. Invalid enabled signals are returned as warnings and left
// disabled so observability configuration cannot invalidate AgentTemplates.
func TelemetryConfigFromProcess() (TelemetryConfig, []error) {
	traces, traceWarning := signalConfigFromProcess(
		"traces", otelTracingEnabled, otelExporterOTLPTracesEndpoint, otelExporterOTLPTracesProtocol,
	)
	logs, logWarning := signalConfigFromProcess(
		"logs", otelLoggingEnabled, otelExporterOTLPLogsEndpoint, otelExporterOTLPLogsProtocol,
	)
	warnings := make([]error, 0, 2)
	if traceWarning != nil {
		warnings = append(warnings, traceWarning)
	}
	if logWarning != nil {
		warnings = append(warnings, logWarning)
	}
	return TelemetryConfig{
		Traces:                  traces,
		Logs:                    logs,
		CaptureSensitiveContent: environmentEnabled(otelCaptureSensitiveContent),
		CaptureRawAPIBodies:     environmentEnabled(otelCaptureRawAPIBodies),
	}, warnings
}

func signalConfigFromProcess(signal, enabledVariable, endpointVariable, protocolVariable string) (SignalConfig, error) {
	if !environmentEnabled(enabledVariable) {
		return SignalConfig{}, nil
	}

	endpoint := strings.TrimSpace(os.Getenv(endpointVariable))
	signalSpecificEndpoint := endpoint != ""
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv(otelExporterOTLPEndpoint))
	}
	if endpoint == "" {
		return SignalConfig{}, fmt.Errorf("OTLP %s endpoint is required when %s export is enabled", signal, signal)
	}

	protocol := strings.ToLower(strings.TrimSpace(os.Getenv(protocolVariable)))
	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(os.Getenv(otelExporterOTLPProtocol)))
	}
	if protocol == "" {
		protocol = defaultOTLPProtocol
	}
	if protocol != "grpc" && protocol != "http/protobuf" {
		return SignalConfig{}, fmt.Errorf("unsupported OTLP %s protocol %q", signal, protocol)
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return SignalConfig{}, fmt.Errorf("OTLP %s endpoint must be an absolute HTTP(S) URL without credentials, query, or fragment", signal)
	}
	if protocol == "http/protobuf" && !signalSpecificEndpoint {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/v1/" + signal
		endpoint = parsed.String()
	}

	return SignalConfig{Enabled: true, Endpoint: endpoint, Protocol: protocol, Hostname: parsed.Hostname()}, nil
}

func environmentEnabled(name string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(name)), "true")
}

// OwnsTelemetryEnvironment reports whether Kagent resolves and compiles the
// variable into runtime revisions. Other OTEL variables remain available for
// harness-specific tuning.
func OwnsTelemetryEnvironment(name string) bool {
	switch name {
	case otelTracingEnabled, otelLoggingEnabled,
		otelExporterOTLPEndpoint, otelExporterOTLPTracesEndpoint, otelExporterOTLPLogsEndpoint,
		otelExporterOTLPProtocol, otelExporterOTLPTracesProtocol, otelExporterOTLPLogsProtocol:
		return true
	default:
		return false
	}
}

// OtelEnvFromProcess returns extra OTEL_* settings the compiled TelemetryConfig
// does not own, so agent runtimes still inherit operator-set limits such as
// OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT.
func OtelEnvFromProcess() []corev1.EnvVar {
	envVars := make([]corev1.EnvVar, 0, len(otelEnvNames))
	for _, name := range otelEnvNames {
		if value, found := os.LookupEnv(name); found && value != "" {
			envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
		}
	}
	return envVars
}

// TraceEnvironment renders the resolved trace settings for an agent runtime.
func (c TelemetryConfig) TraceEnvironment() []corev1.EnvVar {
	return signalEnvironment(c.Traces, otelTracingEnabled, otelExporterOTLPTracesEndpoint, otelExporterOTLPTracesProtocol)
}

// LogEnvironment renders the resolved log settings for an agent runtime.
func (c TelemetryConfig) LogEnvironment() []corev1.EnvVar {
	return signalEnvironment(c.Logs, otelLoggingEnabled, otelExporterOTLPLogsEndpoint, otelExporterOTLPLogsProtocol)
}

func signalEnvironment(config SignalConfig, enabledVariable, endpointVariable, protocolVariable string) []corev1.EnvVar {
	if !config.Enabled {
		return nil
	}
	return []corev1.EnvVar{
		{Name: enabledVariable, Value: "true"},
		{Name: endpointVariable, Value: config.Endpoint},
		{Name: protocolVariable, Value: config.Protocol},
	}
}
