// Package config defines the versioned, non-secret Claude Harness runtime
// configuration shared by its compiler and Actor entrypoint.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

var agentNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const (
	Version = 7
	// PinnedClaudeVersion is the Claude Code release the image installs. A bump
	// must recheck the telemetry gate in the driver.
	PinnedClaudeVersion                 = "2.1.285"
	ClaudeConfigDirEnvName              = "CLAUDE_CONFIG_DIR"
	DisableUpdatesEnvName               = "DISABLE_UPDATES"
	GoogleApplicationCredentialsEnvName = "GOOGLE_APPLICATION_CREDENTIALS"
	UseBedrockEnvName                   = "CLAUDE_CODE_USE_BEDROCK"
	UseVertexEnvName                    = "CLAUDE_CODE_USE_VERTEX"
	SkipVertexAuthEnvName               = "CLAUDE_CODE_SKIP_VERTEX_AUTH"
	AWSRegionEnvName                    = "AWS_REGION"
	AWSAccessKeyEnvName                 = "AWS_ACCESS_KEY_ID"
	AWSSecretKeyEnvName                 = "AWS_SECRET_ACCESS_KEY"
	AWSSessionTokenEnvName              = "AWS_SESSION_TOKEN"
	AWSBedrockTokenEnvName              = "AWS_BEARER_TOKEN_BEDROCK"
	AnthropicAPIKeyEnvName              = "ANTHROPIC_API_KEY"
	AnthropicBaseURLEnvName             = "ANTHROPIC_BASE_URL"
	AnthropicCustomHeadersEnvName       = "ANTHROPIC_CUSTOM_HEADERS"
	MaxOutputTokensEnvName              = "CLAUDE_CODE_MAX_OUTPUT_TOKENS"
	VertexProjectEnvName                = "ANTHROPIC_VERTEX_PROJECT_ID"
	VertexRegionEnvName                 = "CLOUD_ML_REGION"
	SandboxEnvName                      = "IS_SANDBOX"
	PropagateTokenEnvName               = "KAGENT_PROPAGATE_TOKEN"
	HomeEnvName                         = "HOME"
	MCPCredentialEnvPrefix              = "KAGENT_CLAUDE_MCP_CREDENTIAL_"
)

// UnprivilegedUID and UnprivilegedGID are the image's kagent user. A harness
// that runs as root starts Claude Code as this user.
const (
	UnprivilegedUID = 65532
	UnprivilegedGID = 65532
)

// OwnsEnvironment reports whether the compiler or adapter reserves name for
// Claude runtime configuration. Harness authors cannot override these values.
func OwnsEnvironment(name string) bool {
	if strings.HasPrefix(name, MCPCredentialEnvPrefix) {
		return true
	}
	switch name {
	case ClaudeConfigDirEnvName, DisableUpdatesEnvName, GoogleApplicationCredentialsEnvName,
		UseBedrockEnvName, UseVertexEnvName, SkipVertexAuthEnvName, AWSRegionEnvName,
		AWSAccessKeyEnvName, AWSSecretKeyEnvName, AWSSessionTokenEnvName, AWSBedrockTokenEnvName,
		AnthropicAPIKeyEnvName, AnthropicBaseURLEnvName, AnthropicCustomHeadersEnvName, VertexProjectEnvName, VertexRegionEnvName,
		MaxOutputTokensEnvName, SandboxEnvName, "CLAUDE_CODE_ENABLE_TELEMETRY",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA", "OTEL_TRACES_EXPORTER", "OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER", "OTEL_LOG_USER_PROMPTS", "OTEL_LOG_TOOL_DETAILS", "OTEL_LOG_TOOL_CONTENT",
		"OTEL_LOG_ASSISTANT_RESPONSES", "OTEL_LOG_RAW_API_BODIES", "TRACEPARENT", "TRACESTATE",
		"OTEL_EXPORTER_PROMETHEUS_HOST", "OTEL_EXPORTER_PROMETHEUS_PORT",
		"KAGENT_NAME", "KAGENT_NAMESPACE":
		return true
	default:
		return false
	}
}

// CustomHeaders renders headers as the value of ANTHROPIC_CUSTOM_HEADERS: one
// "Name: Value" pair per line, which Claude adds to every model call. The lines
// are sorted by name so the same headers render the same value.
func CustomHeaders(headers map[string]string) string {
	names := slices.Sorted(maps.Keys(headers))
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, name+": "+headers[name])
	}
	return strings.Join(lines, "\n")
}

type Config struct {
	Version               int                    `json:"version"`
	ClaudeExecutable      string                 `json:"claude_executable"`
	ExpectedClaudeVersion string                 `json:"expected_claude_version"`
	StrictVersion         bool                   `json:"strict_version"`
	Model                 string                 `json:"model,omitempty"`
	AppendSystemPrompt    string                 `json:"append_system_prompt,omitempty"`
	Agents                map[string]Agent       `json:"agents,omitempty"`
	SkillResources        *agentplugin.Resources `json:"skill_resources,omitempty"`
	MCPServers            map[string]MCPServer   `json:"mcp_servers,omitempty"`
	// RuntimeTelemetry carries the compiler-owned span identity and content
	// capture policy. It is absent for standalone runs, which fall back to the
	// environment for service identity and leave capture disabled.
	RuntimeTelemetry     tracing.RuntimeTelemetry `json:"runtime_telemetry,omitzero"`
	MaxEventBytes        int                      `json:"max_event_bytes"`
	MaxStderrBytes       int                      `json:"max_stderr_bytes"`
	InterruptGraceMillis int                      `json:"interrupt_grace_millis"`
}

// MCPServer is one compiler-owned direct remote server. Claude's strict MCP
// file is generated by the adapter; no project or user MCP configuration is
// inherited.
type MCPServer struct {
	Type            string            `json:"type"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	RequireApproval bool              `json:"require_approval,omitempty"`
	// TimeoutMillis is Claude's per-server tool-call budget. Zero leaves
	// Claude's default; Claude ignores values below MinMCPTimeoutMillis.
	TimeoutMillis int64 `json:"timeout_millis,omitempty"`
	// Tools is the server's tool selection: the only tools Claude may list and
	// call, enforced by the harness's MCP forwarder. Empty exposes the whole
	// server.
	Tools []string `json:"tools,omitempty"`
}

// MinMCPTimeoutMillis is the smallest per-server timeout Claude Code honours.
const MinMCPTimeoutMillis = 1000

// Agent is one compiler-owned local Claude subagent passed through --agents.
// Permission mode, hooks, memory, isolation, and inline MCP are deliberately
// omitted from the supported contract.
type Agent struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model,omitempty"`
}

// Bounds the memory one stream-json line can take. Tool results carrying
// images run to a few megabytes.
const maxEventBytes = 16 << 20

func Production(model, instruction string) Config {
	return Config{
		Version: Version, ClaudeExecutable: "claude",
		ExpectedClaudeVersion: PinnedClaudeVersion, StrictVersion: true,
		Model: model, AppendSystemPrompt: instruction,
		MaxEventBytes: maxEventBytes, MaxStderrBytes: 64 << 10,
		InterruptGraceMillis: 2000,
	}
}

func Parse(b []byte) (Config, error) {
	var cfg Config
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Config{}, fmt.Errorf("decode config: trailing JSON value")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("unsupported config version %d (want %d)", c.Version, Version)
	}
	if strings.TrimSpace(c.ClaudeExecutable) == "" {
		return fmt.Errorf("claude_executable is required")
	}
	if c.StrictVersion && strings.TrimSpace(c.ExpectedClaudeVersion) == "" {
		return fmt.Errorf("expected_claude_version is required when strict_version is enabled")
	}
	if c.MaxEventBytes <= 0 || c.MaxStderrBytes <= 0 || c.InterruptGraceMillis <= 0 {
		return fmt.Errorf("event, stderr, and interrupt grace limits must be positive")
	}
	if err := c.RuntimeTelemetry.Validate(); err != nil {
		return err
	}
	if runtime := c.RuntimeTelemetry.Runtime; runtime != "" && runtime != tracing.RuntimeClaude {
		return fmt.Errorf("claude runtime telemetry names runtime %q", runtime)
	}
	for name, agent := range c.Agents {
		if !agentNamePattern.MatchString(name) {
			return fmt.Errorf("claude agent name %q must contain only letters, numbers, underscores, or hyphens", name)
		}
		if strings.TrimSpace(agent.Description) == "" || strings.TrimSpace(agent.Prompt) == "" {
			return fmt.Errorf("claude agent %q requires a non-empty description and prompt", name)
		}
	}
	for name, server := range c.MCPServers {
		if !agentNamePattern.MatchString(name) {
			return fmt.Errorf("claude MCP server name %q must contain only letters, numbers, underscores, or hyphens", name)
		}
		if server.Type != "http" && server.Type != "sse" {
			return fmt.Errorf("claude MCP server %q has unsupported transport %q", name, server.Type)
		}
		if strings.TrimSpace(server.URL) == "" {
			return fmt.Errorf("claude MCP server %q URL is required", name)
		}
		for header, value := range server.Headers {
			if strings.TrimSpace(header) == "" || strings.TrimSpace(value) == "" {
				return fmt.Errorf("claude MCP server %q headers require non-empty names and values", name)
			}
		}
		if server.TimeoutMillis != 0 && server.TimeoutMillis < MinMCPTimeoutMillis {
			return fmt.Errorf("claude MCP server %q timeout must be at least %dms", name, MinMCPTimeoutMillis)
		}
		if len(server.Tools) != 0 && server.Type != "http" {
			return fmt.Errorf("claude MCP server %q selects tools, which only a streamable HTTP server supports", name)
		}
		for i, tool := range server.Tools {
			if strings.TrimSpace(tool) == "" || slices.Contains(server.Tools[:i], tool) {
				return fmt.Errorf("claude MCP server %q tool selection requires unique non-empty names", name)
			}
		}
	}
	return nil
}

func (c Config) AgentsJSON() (string, error) {
	if len(c.Agents) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(c.Agents)
	if err != nil {
		return "", fmt.Errorf("encode Claude agents: %w", err)
	}
	return string(raw), nil
}

func (c Config) MCPConfigJSON() ([]byte, error) {
	if len(c.MCPServers) == 0 {
		return nil, nil
	}
	type nativeMCPServer struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers,omitempty"`
		Timeout int64             `json:"timeout,omitempty"`
	}
	servers := make(map[string]nativeMCPServer, len(c.MCPServers))
	for name, server := range c.MCPServers {
		servers[name] = nativeMCPServer{Type: server.Type, URL: server.URL, Headers: server.Headers, Timeout: server.TimeoutMillis}
	}
	raw, err := json.Marshal(struct {
		Servers map[string]nativeMCPServer `json:"mcpServers"`
	}{Servers: servers})
	if err != nil {
		return nil, fmt.Errorf("encode Claude MCP configuration: %w", err)
	}
	return raw, nil
}

func (c Config) InterruptGrace() time.Duration {
	return time.Duration(c.InterruptGraceMillis) * time.Millisecond
}
