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
	otelExporterOTLPEndpoint       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOTLPTracesEndpoint = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	otelExporterOTLPProtocol       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	otelExporterOTLPTracesProtocol = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
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

// TraceConfigFromProcess resolves the standard OTLP trace settings used by all
// harness compilers. Signal-specific settings take precedence over generic
// settings.
func TraceConfigFromProcess() (TraceConfig, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(otelTracingEnabled)), "true") {
		return TraceConfig{}, nil
	}

	endpoint := strings.TrimSpace(os.Getenv(otelExporterOTLPTracesEndpoint))
	traceSpecificEndpoint := endpoint != ""
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv(otelExporterOTLPEndpoint))
	}
	if endpoint == "" {
		return TraceConfig{}, fmt.Errorf("OTLP trace endpoint is required when tracing is enabled")
	}

	protocol := strings.ToLower(strings.TrimSpace(os.Getenv(otelExporterOTLPTracesProtocol)))
	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(os.Getenv(otelExporterOTLPProtocol)))
	}
	if protocol == "" {
		protocol = defaultOTLPProtocol
	}
	switch protocol {
	case "grpc", "http/protobuf":
	default:
		return TraceConfig{}, fmt.Errorf("unsupported OTLP trace protocol %q", protocol)
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return TraceConfig{}, fmt.Errorf("OTLP trace endpoint must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if protocol != "grpc" && !traceSpecificEndpoint {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/v1/traces"
		endpoint = parsed.String()
	}

	return TraceConfig{Enabled: true, Endpoint: endpoint, Protocol: protocol, hostname: parsed.Hostname()}, nil
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
