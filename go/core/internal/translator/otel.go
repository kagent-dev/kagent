package translator

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	corev1 "k8s.io/api/core/v1"
)

const (
	otelSDKDisabled          = "OTEL_SDK_DISABLED"
	otelServiceName          = "OTEL_SERVICE_NAME"
	otelResourceAttributes   = "OTEL_RESOURCE_ATTRIBUTES"
	otelExporterOTLPEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOTLPProtocol = "OTEL_EXPORTER_OTLP_PROTOCOL"
	otelExporterOTLPTimeout  = "OTEL_EXPORTER_OTLP_TIMEOUT"
	otelCaptureRawAPIBodies  = "KAGENT_OTEL_CAPTURE_RAW_API_BODIES"
	otelMaxCaptureBytes      = "KAGENT_OTEL_MAX_CAPTURE_BYTES"
	// OperatorResourceAttributesVariable carries the operator's resource
	// attributes. The controller's own OTEL_RESOURCE_ATTRIBUTES also names its
	// pod, which no runtime may inherit.
	OperatorResourceAttributesVariable = "KAGENT_OTEL_RESOURCE_ATTRIBUTES"
	defaultOTLPProtocol                = "grpc"
)

// Signal is one OpenTelemetry signal the controller configures.
type Signal string

// Signals the controller configures.
const (
	SignalTraces  Signal = "traces"
	SignalMetrics Signal = "metrics"
	SignalLogs    Signal = "logs"
)

var signals = []Signal{SignalTraces, SignalMetrics, SignalLogs}

func (s Signal) exporterVariable() string {
	return "OTEL_" + strings.ToUpper(string(s)) + "_EXPORTER"
}

func (s Signal) endpointVariable() string {
	return "OTEL_EXPORTER_OTLP_" + strings.ToUpper(string(s)) + "_ENDPOINT"
}

func (s Signal) protocolVariable() string {
	return "OTEL_EXPORTER_OTLP_" + strings.ToUpper(string(s)) + "_PROTOCOL"
}

// TelemetryConfig is the controller-owned telemetry configuration compiled
// into runtime revisions. Invalid signals are disabled before compilation.
type TelemetryConfig struct {
	Traces                  SignalConfig
	Metrics                 SignalConfig
	Logs                    SignalConfig
	Endpoint                string
	Protocol                string
	Timeout                 string
	ResourceAttributes      string
	CaptureSensitiveContent bool
	CaptureRawAPIBodies     bool
	// MaxCaptureBytes bounds each captured prompt and response on a Harness
	// invocation span. Zero selects the shared default.
	MaxCaptureBytes int
}

// SignalConfig is the resolved export configuration for one telemetry signal.
type SignalConfig struct {
	Enabled bool
	// Endpoint is the full per-signal URL, for runtimes that take one.
	Endpoint         string
	Protocol         string
	Hostname         string
	EndpointOverride string
	ProtocolOverride string
}

// TelemetryConfigFromProcess resolves the telemetry settings inherited by
// agent runtimes from the controller's own SDK-spec environment. Invalid
// settings are returned as warnings and leave their signal disabled, so
// observability configuration cannot invalidate AgentTemplates.
func TelemetryConfigFromProcess() (TelemetryConfig, []error) {
	config := TelemetryConfig{
		Endpoint:            strings.TrimSpace(os.Getenv(otelExporterOTLPEndpoint)),
		Protocol:            strings.ToLower(strings.TrimSpace(os.Getenv(otelExporterOTLPProtocol))),
		CaptureRawAPIBodies: strings.EqualFold(strings.TrimSpace(os.Getenv(otelCaptureRawAPIBodies)), "true"),
	}
	var warnings []error
	disabled := strings.EqualFold(strings.TrimSpace(os.Getenv(otelSDKDisabled)), "true")
	for _, signal := range signals {
		resolved, err := config.signalFromProcess(signal, disabled)
		if err != nil {
			warnings = append(warnings, err)
		}
		*config.signal(signal) = resolved
	}
	if config.Protocol == "" {
		config.Protocol = defaultOTLPProtocol
	}
	if raw := strings.TrimSpace(os.Getenv(otelExporterOTLPTimeout)); raw != "" {
		if value, err := strconv.Atoi(raw); err != nil || value <= 0 {
			warnings = append(warnings, fmt.Errorf("%s must be a positive number of milliseconds", otelExporterOTLPTimeout))
		} else {
			config.Timeout = raw
		}
	}
	attributes, err := resourceAttributesFromProcess()
	if err != nil {
		warnings = append(warnings, err)
	}
	config.ResourceAttributes = attributes
	switch capture := strings.TrimSpace(os.Getenv(tracing.CaptureContentEnvironmentVariable)); capture {
	case tracing.CaptureContentSpanOnly:
		config.CaptureSensitiveContent = true
	case "", "NO_CONTENT", tracing.CaptureContentDisabled:
	default:
		warnings = append(warnings, fmt.Errorf("%s must be SPAN_ONLY or NO_CONTENT, not %q", tracing.CaptureContentEnvironmentVariable, capture))
	}
	maxCaptureBytes, err := maxCaptureBytesFromProcess()
	if err != nil {
		warnings = append(warnings, err)
	}
	config.MaxCaptureBytes = maxCaptureBytes
	return config, warnings
}

func (c *TelemetryConfig) signal(signal Signal) *SignalConfig {
	switch signal {
	case SignalTraces:
		return &c.Traces
	case SignalMetrics:
		return &c.Metrics
	default:
		return &c.Logs
	}
}

// signalFromProcess resolves one signal. Only an explicit otlp exporter is
// forwarded; the chart always renders one.
func (c TelemetryConfig) signalFromProcess(signal Signal, disabled bool) (SignalConfig, error) {
	switch exporter := strings.TrimSpace(os.Getenv(signal.exporterVariable())); {
	case disabled || exporter == "" || exporter == "none":
		return SignalConfig{}, nil
	case exporter != "otlp":
		return SignalConfig{}, fmt.Errorf("%s must be otlp or none, not %q", signal.exporterVariable(), exporter)
	}
	resolved := SignalConfig{
		EndpointOverride: strings.TrimSpace(os.Getenv(signal.endpointVariable())),
		ProtocolOverride: strings.ToLower(strings.TrimSpace(os.Getenv(signal.protocolVariable()))),
	}
	endpoint := resolved.EndpointOverride
	if endpoint == "" {
		endpoint = c.Endpoint
	}
	if endpoint == "" {
		return SignalConfig{}, fmt.Errorf("OTLP %s endpoint is required when %s export is enabled", signal, signal)
	}
	protocol := resolved.ProtocolOverride
	if protocol == "" {
		protocol = c.Protocol
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
	if protocol == "http/protobuf" && resolved.EndpointOverride == "" {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/v1/" + string(signal)
	}
	resolved.Enabled, resolved.Endpoint, resolved.Protocol, resolved.Hostname = true, parsed.String(), protocol, parsed.Hostname()
	return resolved, nil
}

func resourceAttributesFromProcess() (string, error) {
	raw := strings.TrimSpace(os.Getenv(OperatorResourceAttributesVariable))
	if raw == "" {
		return "", nil
	}
	entries := make([]string, 0, strings.Count(raw, ",")+1)
	var invalid []string
	for entry := range strings.SplitSeq(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			invalid = append(invalid, entry)
			continue
		}
		entries = append(entries, strings.TrimSpace(key)+"="+strings.TrimSpace(value))
	}
	if len(invalid) != 0 {
		return strings.Join(entries, ","), fmt.Errorf("%s ignores entries that are not key=value: %q", OperatorResourceAttributesVariable, invalid)
	}
	return strings.Join(entries, ","), nil
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

// Enabled reports whether any signal is exported.
func (c TelemetryConfig) Enabled() bool {
	return c.Traces.Enabled || c.Metrics.Enabled || c.Logs.Enabled
}

// Destinations are the egress hostnames of the enabled signals.
func (c TelemetryConfig) Destinations() []string {
	var hosts []string
	for _, signal := range []SignalConfig{c.Traces, c.Metrics, c.Logs} {
		if signal.Enabled {
			hosts = append(hosts, signal.Hostname)
		}
	}
	return hosts
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
		return conv.GenAIProviderNameAnthropic
	case v1alpha3.ModelProviderOpenAI:
		return conv.GenAIProviderNameOpenAI
	case v1alpha3.ModelProviderAzureOpenAI:
		return conv.GenAIProviderNameAzureAIOpenAI
	case v1alpha3.ModelProviderBedrock:
		return conv.GenAIProviderNameAWSBedrock
	case v1alpha3.ModelProviderGemini:
		return conv.GenAIProviderNameGCPGemini
	case v1alpha3.ModelProviderGeminiVertexAI, v1alpha3.ModelProviderAnthropicVertexAI:
		return conv.GenAIProviderNameGCPVertexAI
	case v1alpha3.ModelProviderFoundry:
		return conv.GenAIProviderNameAzureAIInference
	case v1alpha3.ModelProviderOllama:
		return "ollama"
	case v1alpha3.ModelProviderSAPAICore:
		return "sap.ai_core"
	default:
		return strings.ToLower(string(provider))
	}
}

// HarnessResourceAttributes is the literal OTEL_RESOURCE_ATTRIBUTES a Harness
// sets. The rendered value keeps its entries under the agent identity.
func HarnessResourceAttributes(harness *v1alpha3.Harness) (string, error) {
	for _, variable := range harness.Spec.Env {
		if variable.Name != otelResourceAttributes {
			continue
		}
		if variable.Value == nil {
			return "", NewValidationError("Harness env %q must be a literal value", otelResourceAttributes)
		}
		return *variable.Value, nil
	}
	return "", nil
}

// IsResourceAttributesVariable reports whether a Harness variable is the one
// TelemetryEnvironment merges instead of copying.
func IsResourceAttributesVariable(name string) bool {
	return name == otelResourceAttributes
}

// OwnsTelemetryEnvironment reports whether Kagent compiles the variable into
// runtime revisions. Other OTEL variables remain available for harness tuning.
func OwnsTelemetryEnvironment(name string) bool {
	switch name {
	case otelSDKDisabled, otelServiceName,
		otelExporterOTLPEndpoint, otelExporterOTLPProtocol, otelExporterOTLPTimeout,
		tracing.CaptureContentEnvironmentVariable:
		return true
	}
	for _, signal := range signals {
		if name == signal.exporterVariable() || name == signal.endpointVariable() || name == signal.protocolVariable() {
			return true
		}
	}
	return false
}

// TelemetryEnvironment renders the SDK-spec environment of one compiled agent.
// Only operator decisions are rendered, because a Substrate actor holds at most
// 32 variables; kagent runtimes apply telemetry.Defaults themselves. The
// capture decision is always rendered, so a runtime that reaches a collector
// through settings the controller did not render still follows it.
func (c TelemetryConfig) TelemetryEnvironment(identity tracing.RuntimeTelemetry, harnessAttributes string) []corev1.EnvVar {
	capture := corev1.EnvVar{Name: tracing.CaptureContentEnvironmentVariable, Value: tracing.CaptureContentDisabled}
	if c.CaptureSensitiveContent {
		capture.Value = tracing.CaptureContentSpanOnly
	}
	resource := corev1.EnvVar{Name: otelResourceAttributes, Value: tracing.MergeResourceAttributes(
		strings.Join([]string{c.ResourceAttributes, harnessAttributes}, ","), resourceIdentity(identity))}
	if !c.Enabled() {
		environment := []corev1.EnvVar{
			{Name: otelSDKDisabled, Value: "true"},
			{Name: SignalTraces.exporterVariable(), Value: "none"},
			{Name: SignalMetrics.exporterVariable(), Value: "none"},
			{Name: SignalLogs.exporterVariable(), Value: "none"},
			capture,
		}
		if harnessAttributes != "" {
			environment = append(environment, resource)
		}
		return environment
	}
	environment := make([]corev1.EnvVar, 0, 16)
	for _, signal := range signals {
		exporter := "none"
		if c.signal(signal).Enabled {
			exporter = "otlp"
		}
		environment = append(environment, corev1.EnvVar{Name: signal.exporterVariable(), Value: exporter})
	}
	sharedEndpoint, sharedProtocol := c.sharedExporter()
	if sharedEndpoint {
		environment = append(environment, corev1.EnvVar{Name: otelExporterOTLPEndpoint, Value: c.Endpoint})
	}
	environment = append(environment, corev1.EnvVar{Name: otelExporterOTLPProtocol, Value: sharedProtocol})
	for _, signal := range signals {
		resolved := c.signal(signal)
		if !resolved.Enabled {
			continue
		}
		if resolved.EndpointOverride != "" {
			environment = append(environment, corev1.EnvVar{Name: signal.endpointVariable(), Value: resolved.EndpointOverride})
		}
		if resolved.Protocol != sharedProtocol {
			environment = append(environment, corev1.EnvVar{Name: signal.protocolVariable(), Value: resolved.Protocol})
		}
	}
	if c.Timeout != "" {
		environment = append(environment, corev1.EnvVar{Name: otelExporterOTLPTimeout, Value: c.Timeout})
	}
	return append(environment, corev1.EnvVar{Name: otelServiceName, Value: identity.AgentName}, resource, capture)
}

// sharedExporter reports whether an enabled signal exports to the shared
// endpoint, and the protocol most enabled signals use, so that each variable
// is rendered once.
func (c TelemetryConfig) sharedExporter() (bool, string) {
	endpoint := false
	counts := map[string]int{}
	for _, signal := range signals {
		resolved := c.signal(signal)
		if !resolved.Enabled {
			continue
		}
		endpoint = endpoint || (resolved.EndpointOverride == "" && c.Endpoint != "")
		counts[resolved.Protocol]++
	}
	protocol := c.Protocol
	for _, candidate := range []string{"grpc", "http/protobuf"} {
		if counts[candidate] > counts[protocol] {
			protocol = candidate
		}
	}
	return endpoint, protocol
}

// DefaultsEnvironment renders telemetry.Defaults for an image that may not
// apply them itself.
func DefaultsEnvironment() []corev1.EnvVar {
	environment := make([]corev1.EnvVar, 0, len(telemetry.Defaults))
	for _, value := range telemetry.Defaults {
		environment = append(environment, corev1.EnvVar{Name: value.Name, Value: value.Value})
	}
	return environment
}

// resourceIdentity leaves kagent.runtime to the runtime, which knows what it is.
func resourceIdentity(identity tracing.RuntimeTelemetry) []attribute.KeyValue {
	attributes := []attribute.KeyValue{
		semconv.ServiceNamespaceKey.String(identity.AgentNamespace),
		conv.GenAIAgentNameKey.String(identity.AgentName),
		conv.GenAIAgentIDKey.String(identity.AgentID()),
	}
	if identity.Provider != "" {
		attributes = append(attributes, conv.GenAIProviderNameKey.String(identity.Provider))
	}
	if identity.Model != "" {
		attributes = append(attributes, conv.GenAIRequestModelKey.String(identity.Model))
	}
	return attributes
}
