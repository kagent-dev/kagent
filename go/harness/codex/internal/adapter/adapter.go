// Package adapter validates and materializes compiler-owned Codex state.
package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/harness/codex/internal/driver"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	sharedtelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const codexHomeEnv = "CODEX_HOME"

// Input contains compiler output and Actor-owned locations used to construct
// the Codex driver.
type Input struct {
	ConfigJSON  []byte
	Workspace   string
	DurableDir  string
	Environment []string
}

// New validates and materializes Codex-owned state, then constructs its driver.
func New(ctx context.Context, input Input) (*driver.ProcessDriver, error) {
	cfg, err := config.Parse(input.ConfigJSON)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(input.Workspace) || !filepath.IsAbs(input.DurableDir) {
		return nil, fmt.Errorf("workspace and durable directories must be absolute paths")
	}
	codexHome := filepath.Join(input.DurableDir, "codex")
	skillsRoot := config.SkillsRoot(input.DurableDir)
	for _, directory := range []string{input.Workspace, codexHome, filepath.Join(codexHome, "agents"), skillsRoot} {
		if err := utils.EnsurePrivateDir(directory); err != nil {
			return nil, fmt.Errorf("prepare Codex directory %q: %w", directory, err)
		}
	}
	if err := reconcileGeneratedDir(filepath.Join(codexHome, "agents"), agentFileNames(cfg.Agents)); err != nil {
		return nil, fmt.Errorf("reconcile Codex agents: %w", err)
	}
	if err := reconcileGeneratedDir(skillsRoot, nil); err != nil {
		return nil, fmt.Errorf("reconcile Codex skills: %w", err)
	}
	if cfg.SkillResources != nil {
		materialized, err := agentplugins.Materialize(ctx, *cfg.SkillResources, agentplugins.Paths{
			Packages: filepath.Join(codexHome, "packages"),
			Skills:   skillsRoot,
		})
		if err != nil {
			return nil, fmt.Errorf("materialize Codex skills: %w", err)
		}
		if len(materialized.ClaudeFormatPluginRoots()) > 0 {
			return nil, fmt.Errorf("plugin is Claude-format; only the Agent Plugins format (plugin.json at the plugin root) is supported here")
		}
	}
	if err := materializeAgents(codexHome, cfg.Agents); err != nil {
		return nil, err
	}
	nativeDefaults := cfg.RuntimeTelemetry
	if cfg.Telemetry != nil {
		exporters := nativeDefaults.ExporterConfig()
		if cfg.Telemetry.Traces != nil {
			exporters.Traces = &sharedtelemetry.OTLPConfig{Endpoint: cfg.Telemetry.Traces.Endpoint, Protocol: cfg.Telemetry.Traces.Protocol}
		}
		if cfg.Telemetry.Logs != nil {
			exporters.Logs = &sharedtelemetry.OTLPConfig{Endpoint: cfg.Telemetry.Logs.Endpoint, Protocol: cfg.Telemetry.Logs.Protocol}
		}
		nativeDefaults.Exporters = &exporters
		nativeDefaults.CaptureContent = cfg.Telemetry.CaptureContent
	}
	environment := nativeEnvironment(input.Environment, codexHome, nativeDefaults)
	cfg.Telemetry = effectiveTelemetry(cfg.Telemetry, environment)
	cfg.Telemetry.CaptureContent = nativeDefaults.WithEnvironment(input.Environment).CaptureContent
	configTOML, err := renderConfig(cfg, codexHome, slices.Contains(input.Environment, env.TestCodexDisableRetries+"=true"))
	if err != nil {
		return nil, err
	}
	if err := utils.ReplacePrivateFile(filepath.Join(codexHome, "config.toml"), configTOML); err != nil {
		return nil, fmt.Errorf("materialize Codex configuration: %w", err)
	}
	approvalServers := make(map[string]struct{})
	for name, server := range cfg.MCPServers {
		if server.RequireApproval {
			approvalServers[name] = struct{}{}
		}
	}
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: cfg.CodexExecutable, ExpectedVersion: cfg.ExpectedCodexVersion, StrictVersion: cfg.StrictVersion,
		Workspace: input.Workspace, Model: cfg.Model, Provider: nativeProviderName(cfg.Provider.Name),
		DeveloperInstruction: cfg.DeveloperInstruction, Environment: environment,
		MaxFrameBytes: cfg.MaxFrameBytes, MaxStderrBytes: cfg.MaxStderrBytes, InterruptGrace: cfg.InterruptGrace(),
		ApprovalServers: approvalServers,
		PendingDir:      filepath.Join(input.DurableDir, "adapter"),
		Tools:           driver.NewMCPCaller(cfg.MCPServers, environment),
	}), nil
}

type nativeConfig struct {
	Model          string                         `toml:"model"`
	ModelProvider  string                         `toml:"model_provider"`
	ApprovalPolicy nativeApprovalPolicy           `toml:"approval_policy"`
	SandboxMode    string                         `toml:"sandbox_mode"`
	WebSearch      string                         `toml:"web_search"`
	Features       nativeFeatures                 `toml:"features"`
	Analytics      nativeAnalytics                `toml:"analytics"`
	Otel           *nativeOtel                    `toml:"otel,omitempty"`
	ModelProviders map[string]nativeModelProvider `toml:"model_providers,omitempty"`
	Agents         map[string]nativeAgent         `toml:"agents,omitempty"`
	MCPServers     map[string]nativeMCPServer     `toml:"mcp_servers,omitempty"`
}

type nativeApprovalPolicy struct {
	Granular nativeGranularApprovalPolicy `toml:"granular"`
}

type nativeGranularApprovalPolicy struct {
	SandboxApproval    bool `toml:"sandbox_approval"`
	Rules              bool `toml:"rules"`
	SkillApproval      bool `toml:"skill_approval"`
	RequestPermissions bool `toml:"request_permissions"`
	MCPElicitations    bool `toml:"mcp_elicitations"`
}

type nativeFeatures struct {
	DefaultModeRequestUserInput bool `toml:"default_mode_request_user_input"`
	UnboundedConnectionRetries  bool `toml:"unbounded_connection_retries"`
}

type nativeAnalytics struct {
	Enabled bool `toml:"enabled"`
}

type nativeOtel struct {
	LogUserPrompt bool                `toml:"log_user_prompt"`
	Exporter      *nativeOtelExporter `toml:"exporter,omitempty"`
	TraceExporter *nativeOtelExporter `toml:"trace_exporter,omitempty"`
}

type nativeOtelExporter struct {
	OTLPGRPC *nativeOTLPGRPC `toml:"otlp-grpc,omitempty"`
	OTLPHTTP *nativeOTLPHTTP `toml:"otlp-http,omitempty"`
}

type nativeOTLPGRPC struct {
	Endpoint string `toml:"endpoint"`
}

type nativeOTLPHTTP struct {
	Endpoint string `toml:"endpoint"`
	Protocol string `toml:"protocol"`
}

type nativeModelProvider struct {
	HTTPHeaders map[string]string `toml:"http_headers,omitempty"`
	Name        string            `toml:"name"`
	WireAPI     string            `toml:"wire_api"`
	EnvKey      string            `toml:"env_key"`
	BaseURL     string            `toml:"base_url,omitempty"`
	// TODO: REMOVE these test-only retry budgets once upstream denial handling permits
	// bounded native retries. Alpha3 fixes DNS search fallback, not native backoff:
	// https://github.com/agent-substrate/substrate/issues/2663
	// Restore native defaults by omitting both fields, not by leaving zeros.
	RequestMaxRetries *uint `toml:"request_max_retries,omitempty"`
	StreamMaxRetries  *uint `toml:"stream_max_retries,omitempty"`
}

type nativeAgent struct {
	Description string `toml:"description"`
	ConfigFile  string `toml:"config_file"`
}

type nativeMCPServer struct {
	URL                      string            `toml:"url"`
	HTTPHeaders              map[string]string `toml:"http_headers,omitempty,inline"`
	EnvHTTPHeaders           map[string]string `toml:"env_http_headers,omitempty,inline"`
	EnabledTools             []string          `toml:"enabled_tools,omitempty"`
	DefaultToolsApprovalMode string            `toml:"default_tools_approval_mode"`
}

type nativeAgentConfig struct {
	Model                 string `toml:"model"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

// renderConfig translates the versioned Kagent contract into the pinned
// Codex CLI's native TOML without copying credential values into durable state.
func renderConfig(cfg config.Config, codexHome string, disableRetries bool) ([]byte, error) {
	native := nativeConfig{
		Model: cfg.Model, ModelProvider: nativeProviderName(cfg.Provider.Name),
		ApprovalPolicy: nativeApprovalPolicy{Granular: nativeGranularApprovalPolicy{MCPElicitations: true}},
		SandboxMode:    "danger-full-access", WebSearch: "cached",
		// DNS denials look like connection failures. Keep retries bounded even
		// after the temporary zero provider retry budgets are removed.
		Features:   nativeFeatures{DefaultModeRequestUserInput: true, UnboundedConnectionRetries: false},
		Analytics:  nativeAnalytics{Enabled: false},
		Agents:     make(map[string]nativeAgent, len(cfg.Agents)),
		MCPServers: make(map[string]nativeMCPServer, len(cfg.MCPServers)),
	}
	if cfg.Provider.Name == "openai" {
		provider := nativeModelProvider{
			HTTPHeaders: cfg.ModelHeaders,
			Name:        "OpenAI", WireAPI: "responses", EnvKey: "OPENAI_API_KEY", BaseURL: cfg.Provider.BaseURL,
		}
		if disableRetries {
			provider.RequestMaxRetries = new(uint(0))
			provider.StreamMaxRetries = new(uint(0))
		}
		native.ModelProviders = map[string]nativeModelProvider{
			"kagent-openai": provider,
		}
	}
	if cfg.Telemetry != nil {
		native.Otel = &nativeOtel{
			LogUserPrompt: cfg.Telemetry.CaptureContent,
			Exporter:      nativeExporter(cfg.Telemetry.Logs),
			TraceExporter: nativeExporter(cfg.Telemetry.Traces),
		}
	}
	for name, agent := range cfg.Agents {
		native.Agents[name] = nativeAgent{
			Description: agent.Description,
			ConfigFile:  filepath.Join(codexHome, "agents", name+".toml"),
		}
	}
	for name, server := range cfg.MCPServers {
		literal, environment := map[string]string{}, map[string]string{}
		for header, value := range server.Headers {
			if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
				environment[header] = strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
			} else {
				literal[header] = value
			}
		}
		approvalMode := "approve"
		if server.RequireApproval {
			approvalMode = "prompt"
		}
		native.MCPServers[name] = nativeMCPServer{
			URL: server.URL, HTTPHeaders: literal, EnvHTTPHeaders: environment, EnabledTools: server.EnabledTools,
			DefaultToolsApprovalMode: approvalMode,
		}
	}
	contents, err := toml.Marshal(native)
	if err != nil {
		return nil, fmt.Errorf("encode Codex configuration: %w", err)
	}
	return contents, nil
}

func nativeExporter(exporterConfig *config.OTLPExporter) *nativeOtelExporter {
	if exporterConfig == nil {
		return nil
	}
	exporter := &nativeOtelExporter{}
	switch exporterConfig.Protocol {
	case "grpc":
		exporter.OTLPGRPC = &nativeOTLPGRPC{Endpoint: exporterConfig.Endpoint}
	case "http/protobuf":
		exporter.OTLPHTTP = &nativeOTLPHTTP{Endpoint: exporterConfig.Endpoint, Protocol: "binary"}
	}
	return exporter
}

func nativeProviderName(name string) string {
	if name == "openai" {
		return "kagent-openai"
	}
	return name
}

// materializeAgents writes the per-agent config files referenced by config.toml.
func materializeAgents(codexHome string, agents map[string]config.Agent) error {
	for name, agent := range agents {
		contents, err := toml.Marshal(nativeAgentConfig{Model: agent.Model, DeveloperInstructions: agent.Instruction})
		if err != nil {
			return fmt.Errorf("encode Codex agent %q configuration: %w", name, err)
		}
		if err := utils.ReplacePrivateFile(filepath.Join(codexHome, "agents", name+".toml"), contents); err != nil {
			return fmt.Errorf("materialize Codex agent %q: %w", name, err)
		}
	}
	return nil
}

func agentFileNames(agents map[string]config.Agent) map[string]struct{} {
	files := make(map[string]struct{}, len(agents))
	for name := range agents {
		files[name+".toml"] = struct{}{}
	}
	return files
}

// reconcileGeneratedDir removes compiler-owned files that are no longer in the
// desired config. Symlinks are rejected so cleanup cannot escape the directory.
func reconcileGeneratedDir(directory string, keep map[string]struct{}) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := keep[entry.Name()]; ok {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("generated path %q is a symlink", path)
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

// nativeEnvironment drops OTLP compression: the Codex exporter is built without
// gzip and refuses to start when asked for it.
func nativeEnvironment(environment []string, codexHome string, telemetry tracing.RuntimeTelemetry) []string {
	environment = tracing.ResourceEnvironment(setEnvironment(sharedtelemetry.Environment(environment, telemetry.ExporterConfig()), codexHomeEnv, codexHome), telemetry.ChildResource())
	return slices.DeleteFunc(environment, func(item string) bool {
		return strings.HasPrefix(item, env.OTELExporterOTLPCompression.Name()+"=")
	})
}

func setEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func effectiveTelemetry(defaults *config.Telemetry, environment []string) *config.Telemetry {
	result := &config.Telemetry{}
	if defaults != nil {
		result.CaptureContent = defaults.CaptureContent
	}
	for signal, target := range map[string]**config.OTLPExporter{"TRACES": &result.Traces, "LOGS": &result.Logs} {
		if exporter := sharedtelemetry.OTLPExporter(environment, signal); exporter != nil {
			*target = &config.OTLPExporter{Endpoint: exporter.Endpoint, Protocol: exporter.Protocol}
		}
	}
	return result
}
