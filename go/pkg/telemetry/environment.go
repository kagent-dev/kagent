package telemetry

import (
	"cmp"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

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
	addDefault("OTEL_PROPAGATORS", "tracecontext")
	addDefault("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip")
	addDefault("OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", "base2_exponential_bucket_histogram")
	for _, signal := range []struct {
		name   string
		config *OTLPConfig
	}{{"TRACES", cfg.Traces}, {"METRICS", cfg.Metrics}, {"LOGS", cfg.Logs}} {
		prefix := "OTEL_EXPORTER_OTLP_" + signal.name + "_"
		exporter := "none"
		if signal.config != nil || supplied(prefix+"ENDPOINT") || supplied("OTEL_EXPORTER_OTLP_ENDPOINT") {
			exporter = "otlp"
		}
		addDefault("OTEL_"+signal.name+"_EXPORTER", exporter)
		if signal.config == nil {
			continue
		}
		for _, option := range []struct{ name, value string }{
			{"ENDPOINT", signal.config.Endpoint},
			{"PROTOCOL", signal.config.Protocol},
			{"TIMEOUT", strconv.FormatInt(signal.config.timeout().Milliseconds(), 10)},
		} {
			if !supplied("OTEL_EXPORTER_OTLP_" + option.name) {
				addDefault(prefix+option.name, option.value)
			}
		}
	}
	attributes := make([]string, 0, len(cfg.ResourceAttributes))
	for key, value := range cfg.ResourceAttributes {
		attributes = append(attributes, key+"="+url.PathEscape(value))
	}
	slices.Sort(attributes)
	if len(attributes) > 0 {
		// The SDK resolves duplicate attributes in order, so env wins per key.
		if value := values["OTEL_RESOURCE_ATTRIBUTES"]; value != "" {
			attributes = append(attributes, value)
		}
		result = slices.DeleteFunc(result, func(entry string) bool { return strings.HasPrefix(entry, "OTEL_RESOURCE_ATTRIBUTES=") })
		result = append(result, "OTEL_RESOURCE_ATTRIBUTES="+strings.Join(attributes, ","))
	}
	return result
}

// SignalEnabled reports native SDK signal selection after Environment has
// applied file defaults. Upstream SDKs handle exporter-specific validation.
func SignalEnabled(environment []string, signal string) bool {
	values := map[string]string{}
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	if strings.EqualFold(strings.TrimSpace(values["OTEL_SDK_DISABLED"]), "true") {
		return false
	}
	value := strings.TrimSpace(values["OTEL_"+signal+"_EXPORTER"])
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
	exporter := cmp.Or(values["OTEL_"+signal+"_EXPORTER"], "otlp")
	if !slices.Contains(strings.Split(exporter, ","), "otlp") {
		return nil
	}
	prefix := "OTEL_EXPORTER_OTLP_" + signal + "_"
	protocol := cmp.Or(values[prefix+"PROTOCOL"], values["OTEL_EXPORTER_OTLP_PROTOCOL"], "grpc")
	endpoint := values[prefix+"ENDPOINT"]
	if endpoint == "" {
		endpoint = values["OTEL_EXPORTER_OTLP_ENDPOINT"]
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
