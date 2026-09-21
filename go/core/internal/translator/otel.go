package translator

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
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
	otelMaxCaptureBytes            = "KAGENT_OTEL_MAX_CAPTURE_BYTES"
	defaultOTLPProtocol            = "grpc"
)

// TelemetryConfig is the controller-owned telemetry configuration compiled
// into runtime revisions. Invalid signals are disabled before compilation.
type TelemetryConfig struct {
	Traces                  SignalConfig
	Logs                    SignalConfig
	CaptureSensitiveContent bool
	CaptureRawAPIBodies     bool
	// MaxCaptureBytes bounds each captured prompt and response on a Harness
	// invocation span. Zero selects the shared default.
	MaxCaptureBytes int
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
	warnings := make([]error, 0, 3)
	if traceWarning != nil {
		warnings = append(warnings, traceWarning)
	}
	if logWarning != nil {
		warnings = append(warnings, logWarning)
	}
	maxCaptureBytes, captureWarning := maxCaptureBytesFromProcess()
	if captureWarning != nil {
		warnings = append(warnings, captureWarning)
	}
	return TelemetryConfig{
		Traces:                  traces,
		Logs:                    logs,
		CaptureSensitiveContent: environmentEnabled(otelCaptureSensitiveContent),
		CaptureRawAPIBodies:     environmentEnabled(otelCaptureRawAPIBodies),
		MaxCaptureBytes:         maxCaptureBytes,
	}, warnings
}

// maxCaptureBytesFromProcess resolves the user's capture budget. An unusable
// value is reported and replaced by the default so an observability setting
// cannot invalidate AgentTemplates.
func maxCaptureBytesFromProcess() (int, error) {
	raw := strings.TrimSpace(os.Getenv(otelMaxCaptureBytes))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > tracing.MaxCaptureBytes {
		return 0, fmt.Errorf("%s must be a positive integer of at most %d bytes", otelMaxCaptureBytes, tracing.MaxCaptureBytes)
	}
	return value, nil
}

// RuntimeTelemetry is the compiler-owned telemetry contract for one compiled
// agent. Identity is what the runtime reports on every invocation span and on
// its resource, including the model the agent is bound to, so usage a native
// runtime reports without naming the model can still be attributed.
func (c TelemetryConfig) RuntimeTelemetry(runtime tracing.Runtime, agentName, namespace string, model v1alpha3.ModelConfigSpec) tracing.RuntimeTelemetry {
	return tracing.RuntimeTelemetry{
		Runtime: runtime, AgentName: agentName, AgentNamespace: namespace,
		Provider: ProviderName(model.Provider), Model: strings.TrimSpace(model.Model),
		CaptureContent: c.CaptureSensitiveContent, MaxCaptureBytes: c.MaxCaptureBytes,
	}
}

// ProviderName maps a ModelConfig provider onto the GenAI conventions'
// provider vocabulary. Providers the conventions do not list get a lowercase
// identifier of the same shape, which the conventions permit as a custom value.
func ProviderName(provider v1alpha3.ModelProvider) string {
	switch provider {
	case v1alpha3.ModelProviderAnthropic:
		return semconv.GenAIProviderNameAnthropic.Value.AsString()
	case v1alpha3.ModelProviderOpenAI:
		return semconv.GenAIProviderNameOpenAI.Value.AsString()
	case v1alpha3.ModelProviderAzureOpenAI:
		return semconv.GenAIProviderNameAzureAIOpenAI.Value.AsString()
	case v1alpha3.ModelProviderBedrock:
		return semconv.GenAIProviderNameAWSBedrock.Value.AsString()
	case v1alpha3.ModelProviderGemini:
		return semconv.GenAIProviderNameGCPGemini.Value.AsString()
	case v1alpha3.ModelProviderGeminiVertexAI, v1alpha3.ModelProviderAnthropicVertexAI:
		return semconv.GenAIProviderNameGCPVertexAI.Value.AsString()
	case v1alpha3.ModelProviderFoundry:
		return semconv.GenAIProviderNameAzureAIInference.Value.AsString()
	case v1alpha3.ModelProviderOllama:
		return "ollama"
	case v1alpha3.ModelProviderSAPAICore:
		return "sap.ai_core"
	default:
		return strings.ToLower(string(provider))
	}
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
		otelExporterOTLPProtocol, otelExporterOTLPTracesProtocol, otelExporterOTLPLogsProtocol,
		tracing.CaptureContentEnvironmentVariable:
		return true
	default:
		return false
	}
}

// TraceEnvironment renders the resolved trace settings for an agent runtime.
// The content-capture switch rides with them, because the standard GenAI
// instrumentation variable is how a runtime that instruments its own model
// calls learns the decision, and a user-supplied value would let one runtime
// record prompts that the controller's setting said to keep out of traces.
func (c TelemetryConfig) TraceEnvironment() []corev1.EnvVar {
	environment := signalEnvironment(c.Traces, otelTracingEnabled, otelExporterOTLPTracesEndpoint, otelExporterOTLPTracesProtocol)
	if environment == nil {
		return nil
	}
	return append(environment, corev1.EnvVar{
		Name: tracing.CaptureContentEnvironmentVariable, Value: strconv.FormatBool(c.CaptureSensitiveContent),
	})
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
