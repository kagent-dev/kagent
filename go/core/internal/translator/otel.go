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
	defaultOTLPProtocol            = "grpc"
)

// TraceConfig is the controller-owned trace export configuration compiled into
// each runtime revision.
type TraceConfig struct {
	Enabled  bool
	Endpoint string
	Protocol string
	hostname string
}

// LogConfig is the controller-owned log export configuration compiled into
// each runtime revision.
type LogConfig struct {
	Enabled  bool
	Endpoint string
	Protocol string
	hostname string
}

// TraceConfigFromProcess resolves the standard OTLP trace settings used by all
// harness compilers. Signal-specific settings take precedence over generic
// settings.
func TraceConfigFromProcess() (TraceConfig, error) {
	config, err := signalConfigFromProcess(otelTracingEnabled, otelExporterOTLPTracesEndpoint, otelExporterOTLPTracesProtocol, "traces")
	if err != nil {
		return TraceConfig{}, err
	}
	return TraceConfig(config), nil
}

// LogConfigFromProcess resolves the standard OTLP log settings used by all
// harness compilers. Signal-specific settings take precedence over generic
// settings.
func LogConfigFromProcess() (LogConfig, error) {
	config, err := signalConfigFromProcess(otelLoggingEnabled, otelExporterOTLPLogsEndpoint, otelExporterOTLPLogsProtocol, "logs")
	if err != nil {
		return LogConfig{}, err
	}
	return LogConfig(config), nil
}

type signalConfig struct {
	Enabled  bool
	Endpoint string
	Protocol string
	hostname string
}

// signalConfigFromProcess resolves the standard OTLP settings used by all
// harness compilers. Signal-specific settings take precedence over generic
// settings.
func signalConfigFromProcess(enabledVariable, endpointVariable, protocolVariable, signal string) (signalConfig, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(enabledVariable)), "true") {
		return signalConfig{}, nil
	}

	endpoint := strings.TrimSpace(os.Getenv(endpointVariable))
	signalSpecificEndpoint := endpoint != ""
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv(otelExporterOTLPEndpoint))
	}
	if endpoint == "" {
		return signalConfig{}, fmt.Errorf("OTLP %s endpoint is required when %s export is enabled", signal, signal)
	}

	protocol := strings.ToLower(strings.TrimSpace(os.Getenv(protocolVariable)))
	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(os.Getenv(otelExporterOTLPProtocol)))
	}
	if protocol == "" {
		protocol = defaultOTLPProtocol
	}
	switch protocol {
	case "grpc", "http/protobuf":
	default:
		return signalConfig{}, fmt.Errorf("unsupported OTLP %s protocol %q", signal, protocol)
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return signalConfig{}, fmt.Errorf("OTLP %s endpoint must be an absolute HTTP(S) URL without credentials, query, or fragment", signal)
	}
	if protocol != "grpc" && !signalSpecificEndpoint {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/v1/" + signal
		endpoint = parsed.String()
	}

	return signalConfig{Enabled: true, Endpoint: endpoint, Protocol: protocol, hostname: parsed.Hostname()}, nil
}

// Environment renders the standard settings consumed by the Go runtime
// telemetry initializer.
func (c TraceConfig) Environment() []corev1.EnvVar {
	if !c.Enabled {
		return nil
	}
	return []corev1.EnvVar{
		{Name: otelTracingEnabled, Value: "true"},
		{Name: otelExporterOTLPTracesEndpoint, Value: c.Endpoint},
		{Name: otelExporterOTLPTracesProtocol, Value: c.Protocol},
	}
}

// CollectorHostname returns the hostname that must be reachable from the
// runtime revision.
func (c TraceConfig) CollectorHostname() string {
	return c.hostname
}

// Environment renders the standard settings consumed by runtime log
// providers and native CLI exporters.
func (c LogConfig) Environment() []corev1.EnvVar {
	if !c.Enabled {
		return nil
	}
	return []corev1.EnvVar{
		{Name: otelLoggingEnabled, Value: "true"},
		{Name: otelExporterOTLPLogsEndpoint, Value: c.Endpoint},
		{Name: otelExporterOTLPLogsProtocol, Value: c.Protocol},
	}
}

// CollectorHostname returns the hostname that must be reachable from the
// runtime revision.
func (c LogConfig) CollectorHostname() string {
	return c.hostname
}
