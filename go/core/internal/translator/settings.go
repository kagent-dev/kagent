package translator

import (
	"fmt"
	"net/url"

	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/api/adk"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

// CompilerSettings contains only deployment inputs needed by harness compilers.
// It is resolved once at startup, independently of reconciliation and env.
type CompilerSettings struct {
	APIURL     string
	GatewayURL string
	LogLevel   string
	Telemetry  TelemetryConfig
	Process    *adk.RuntimeConfig
	Images     BuiltinImages
}

func NewCompilerSettings(cfg *apiv1.RuntimeDefaultsConfig) (CompilerSettings, error) {
	if cfg == nil {
		return CompilerSettings{}, fmt.Errorf("runtime defaults are required")
	}
	if err := config.Validate(cfg); err != nil {
		return CompilerSettings{}, err
	}
	telemetry, err := TelemetryConfigFromSettings(cfg.GetTelemetry())
	if err != nil {
		return CompilerSettings{}, err
	}
	return CompilerSettings{APIURL: cfg.GetControlPlaneUrl(), GatewayURL: cfg.GetGatewayUrl(), LogLevel: cfg.GetLogging().GetLevel(), Telemetry: telemetry, Process: &adk.RuntimeConfig{
		// Preserve presence: nil selects the runtime default; zero disables the limit.
		MaxContentLength: cfg.MaxContentLength,
		FileSearchTools:  cfg.GetFileSearchTools(),
		BashVenvPath:     cfg.GetBashVenvPath(),
		TokenPropagation: proto.CloneOf(cfg.GetTokenPropagation()),
		Telemetry:        proto.CloneOf(cfg.GetTelemetry()),
	}}, nil
}

func TelemetryConfigFromSettings(cfg *apiv1.TelemetryConfig) (TelemetryConfig, error) {
	result := TelemetryConfig{Runtime: config.RuntimeTelemetry(cfg)}
	if cfg == nil {
		return result, nil
	}
	for signal, source := range map[Signal]*apiv1.OTLPConfig{SignalTraces: cfg.GetTraces(), SignalMetrics: cfg.GetMetrics(), SignalLogs: cfg.GetLogs()} {
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

// RuntimeConfig supplies process settings for root ADK and BYO JSON only.
func (s CompilerSettings) RuntimeConfig(name, namespace string) *adk.RuntimeConfig {
	result := proto.CloneOf(s.Process)
	if result == nil {
		result = &adk.RuntimeConfig{}
	}
	result.ControlPlaneUrl, result.GatewayUrl = s.APIURL, s.GatewayURL
	result.Name, result.Namespace = name, namespace
	result.LogLevel, result.Port = s.LogLevel, new(uint32(80))
	return result
}
