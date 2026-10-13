package telemetry

import (
	"cmp"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

var signalEnvironment = map[string]env.OTELSignalVars{
	"TRACES":  env.OTELTraces,
	"METRICS": env.OTELMetrics,
	"LOGS":    env.OTELLogs,
}

// Environment adapts file settings for the SDK and native child processes.
// Explicit process settings win, including a generic OTLP override over a
// signal-specific file value. It never reads the controller's environment while
// compiling managed runtime configuration.
func Environment(environment []string, cfg Config) []string {
	result := slices.Clone(environment)
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	supplied := func(name string) bool { _, ok := values[name]; return ok }
	addDefault := func(name, value string) {
		if !supplied(name) {
			result = append(result, name+"="+value)
		}
	}
	addDefault(env.OTELPropagators.Name(), "tracecontext")
	addDefault(env.OTELExporterOTLPCompression.Name(), "gzip")
	addDefault(env.OTELMetricsDefaultHistogramAggregation.Name(), "base2_exponential_bucket_histogram")
	signals := []struct {
		vars   env.OTELSignalVars
		config *OTLPConfig
	}{{env.OTELTraces, cfg.Traces}, {env.OTELMetrics, cfg.Metrics}, {env.OTELLogs, cfg.Logs}}
	connectionOverridden := func(vars env.OTELSignalVars) bool {
		return supplied(vars.Endpoint.Name()) || supplied(vars.Protocol.Name()) ||
			supplied(env.OTELExporterOTLPEndpoint.Name()) || supplied(env.OTELExporterOTLPProtocol.Name())
	}
	// A shared gRPC destination needs only two variables. HTTP file endpoints
	// include signal paths, so keep those signal-specific. Never promote defaults
	// across a signal with an explicit connection override.
	shared := cfg.Traces
	for _, signal := range signals {
		if shared == nil || signal.config == nil || shared.Protocol != "grpc" ||
			signal.config.Protocol != shared.Protocol || signal.config.Endpoint != shared.Endpoint || connectionOverridden(signal.vars) {
			shared = nil
			break
		}
	}
	if shared != nil {
		addDefault(env.OTELExporterOTLPEndpoint.Name(), shared.Endpoint)
		addDefault(env.OTELExporterOTLPProtocol.Name(), shared.Protocol)
	}
	for _, signal := range signals {
		exporter := "none"
		if signal.config != nil || supplied(signal.vars.Endpoint.Name()) || supplied(env.OTELExporterOTLPEndpoint.Name()) {
			exporter = "otlp"
		}
		addDefault(signal.vars.Exporter.Name(), exporter)
		if signal.config == nil {
			continue
		}
		// Endpoint and protocol form one connection. An env override of either
		// replaces the file pair; the SDK defaults any missing counterpart.
		if shared == nil && !connectionOverridden(signal.vars) {
			addDefault(signal.vars.Endpoint.Name(), signal.config.Endpoint)
			addDefault(signal.vars.Protocol.Name(), signal.config.Protocol)
		}
		if timeout := signal.config.timeout(); timeout != 10*time.Second && !supplied(env.OTELExporterOTLPTimeout.Name()) {
			addDefault(signal.vars.Timeout.Name(), strconv.FormatInt(timeout.Milliseconds(), 10))
		}
	}
	attributes := make([]string, 0, len(cfg.ResourceAttributes))
	for key, value := range cfg.ResourceAttributes {
		attributes = append(attributes, key+"="+url.PathEscape(value))
	}
	slices.Sort(attributes)
	if len(attributes) > 0 {
		// The SDK resolves duplicate attributes in order, so env wins per key.
		if value := values[env.OTELResourceAttributes.Name()]; value != "" {
			attributes = append(attributes, value)
		}
		prefix := env.OTELResourceAttributes.Name() + "="
		result = slices.DeleteFunc(result, func(entry string) bool { return strings.HasPrefix(entry, prefix) })
		result = append(result, prefix+strings.Join(attributes, ","))
	}
	return result
}

// SignalEnabled reports native SDK signal selection after Environment has
// applied file defaults. Upstream SDKs handle exporter-specific validation.
func SignalEnabled(environment []string, signal string) bool {
	names, ok := signalEnvironment[signal]
	if !ok {
		return false
	}
	values := map[string]string{}
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	if strings.EqualFold(strings.TrimSpace(values[env.OTELSDKDisabled.Name()]), "true") {
		return false
	}
	value := strings.TrimSpace(values[names.Exporter.Name()])
	return value != "none"
}

// OTLPExporter resolves the endpoint and protocol for native SDK adapters and
// compiler egress policy. Call Environment first to apply file defaults.
func OTLPExporter(environment []string, signal string) *OTLPConfig {
	if !SignalEnabled(environment, signal) {
		return nil
	}
	values := map[string]string{}
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	names := signalEnvironment[signal]
	exporter := cmp.Or(values[names.Exporter.Name()], "otlp")
	if !slices.Contains(strings.Split(exporter, ","), "otlp") {
		return nil
	}
	protocol := cmp.Or(values[names.Protocol.Name()], values[env.OTELExporterOTLPProtocol.Name()], "grpc")
	endpoint := values[names.Endpoint.Name()]
	if endpoint == "" {
		endpoint = values[env.OTELExporterOTLPEndpoint.Name()]
		if endpoint == "" {
			if protocol == "grpc" {
				endpoint = "http://localhost:4317"
			} else {
				endpoint = "http://localhost:4318"
			}
		}
		if protocol == "http/protobuf" {
			endpoint = strings.TrimRight(endpoint, "/") + "/v1/" + strings.ToLower(signal)
		}
	}
	return &OTLPConfig{Endpoint: endpoint, Protocol: protocol}
}
