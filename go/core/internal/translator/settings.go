package translator

import (
	"fmt"
	"net/url"

	"google.golang.org/protobuf/proto"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TelemetryConfigFromSettings(cfg *configv1.TelemetryConfig) (TelemetryConfig, error) {
	result := TelemetryConfig{Runtime: config.RuntimeTelemetry(cfg)}
	if cfg == nil {
		return result, nil
	}
	for signal, source := range map[Signal]*configv1.OTLPConfig{SignalTraces: cfg.GetTraces(), SignalMetrics: cfg.GetMetrics(), SignalLogs: cfg.GetLogs()} {
		if source == nil {
			continue
		}
		endpoint, err := url.Parse(source.GetEndpoint())
		if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			return TelemetryConfig{}, fmt.Errorf("telemetry.%s.endpoint must be an HTTP(S) URL", signal)
		}
		*result.signal(signal) = SignalConfig{Enabled: true, Endpoint: source.GetEndpoint(),
			Protocol: source.GetProtocol(), Hostname: endpoint.Hostname()}
	}
	return result, nil
}

// RuntimeConfig builds the per-agent ADK or BYO process configuration from the
// controller's validated runtime defaults, without reading process environment.
func RuntimeConfig(cfg *configv1.RuntimeDefaultsConfig, name, namespace string) *configv1.ADKRuntimeConfig {
	return &configv1.ADKRuntimeConfig{
		ControlPlaneUrl: cfg.GetControlPlaneUrl(), GatewayUrl: cfg.GetGatewayUrl(),
		Name: name, Namespace: namespace, LogLevel: cfg.GetLogging().GetLevel(), Port: new(uint32(80)),
		// Preserve optional presence: zero disables the runtime's request limit.
		MaxContentLength: cfg.MaxContentLength,
		BuiltinTools:     proto.CloneOf(cfg.GetBuiltinTools()),
		TokenPropagation: proto.CloneOf(cfg.GetTokenPropagation()),
		Telemetry:        proto.CloneOf(cfg.GetTelemetry()),
		TaskStore:        taskStoreSettings(cfg.GetTaskStore()),
	}
}

// taskStoreSettings drops an empty block rather than copying it: protojson
// still emits a present empty message, which a runtime image that predates the
// field rejects, and the compiled config would no longer match a controller
// without the setting.
func taskStoreSettings(cfg *configv1.TaskStoreConfig) *configv1.TaskStoreConfig {
	if cfg.GetOperationBudget() == nil && cfg.GetAttemptTimeout() == nil {
		return nil
	}
	return proto.CloneOf(cfg)
}
