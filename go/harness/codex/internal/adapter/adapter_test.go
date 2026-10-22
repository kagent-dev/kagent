package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

func TestNewMaterializesCompilerOwnedConfiguration(t *testing.T) {
	durable := filepath.Join(t.TempDir(), "data")
	cfg := config.Production("gpt-5.2-codex", "line one\nline two")
	cfg.Provider = config.Provider{Name: "openai", BaseURL: "https://gateway.example.com/v1"}
	cfg.Agents = map[string]config.Agent{"reviewer": {Description: "Review carefully", Instruction: "Inspect \"all\" changes", Model: "gpt-5.2-codex"}}
	cfg.MCPServers = map[string]config.MCPServer{"tools": {
		URL: "https://mcp.example.com/mcp", Headers: map[string]string{"X-Tenant": "test", "Authorization": "${KAGENT_CODEX_MCP_CREDENTIAL_ABC}"}, EnabledTools: []string{"read"}, RequireApproval: true,
	}}
	cfg.Telemetry = &config.Telemetry{
		CaptureContent: true,
		Traces:         &config.OTLPExporter{Endpoint: "http://collector:4318/v1/traces", Protocol: "http/protobuf"},
		Logs:           &config.OTLPExporter{Endpoint: "http://logs:4317", Protocol: "grpc"},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(context.Background(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable, Environment: []string{"PATH=/bin", "CODEX_HOME=/wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	if runner == nil {
		t.Fatal("New() returned nil")
	}
	codexHome := filepath.Join(durable, "codex")
	configPath := filepath.Join(codexHome, "config.toml")
	agentPath := filepath.Join(codexHome, "agents", "reviewer.toml")
	for _, path := range []string{configPath, agentPath} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions = %v, %v", path, info, err)
		}
	}
	configContents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(configContents, []byte("environment =")) {
		t.Fatalf("generated OTEL configuration overrides Codex's environment default:\n%s", configContents)
	}
	// Seed the upstream default so an omitted false value fails this check.
	native := nativeConfig{Features: nativeFeatures{UnboundedConnectionRetries: true}}
	if err := toml.Unmarshal(configContents, &native); err != nil {
		t.Fatalf("decode generated Codex configuration: %v", err)
	}
	provider := native.ModelProviders["kagent-openai"]
	server := native.MCPServers["tools"]
	agent := native.Agents["reviewer"]
	if native.ModelProvider != "kagent-openai" || provider.WireAPI != "responses" || provider.EnvKey != "OPENAI_API_KEY" || provider.BaseURL != cfg.Provider.BaseURL {
		t.Fatalf("generated provider configuration = %#v, provider name = %q", provider, native.ModelProvider)
	}
	if !native.Features.DefaultModeRequestUserInput {
		t.Fatal("generated Codex configuration does not enable request_user_input in default mode")
	}
	if native.Features.UnboundedConnectionRetries {
		t.Fatal("generated Codex configuration does not disable unbounded connection retries")
	}
	if native.Otel == nil || !native.Otel.LogUserPrompt ||
		native.Otel.TraceExporter == nil || native.Otel.TraceExporter.OTLPHTTP == nil ||
		native.Otel.TraceExporter.OTLPHTTP.Endpoint != cfg.Telemetry.Traces.Endpoint || native.Otel.TraceExporter.OTLPHTTP.Protocol != "binary" ||
		native.Otel.Exporter == nil || native.Otel.Exporter.OTLPGRPC == nil || native.Otel.Exporter.OTLPGRPC.Endpoint != cfg.Telemetry.Logs.Endpoint {
		t.Fatalf("generated OTEL configuration = %#v", native.Otel)
	}
	if native.ApprovalPolicy.Granular != (nativeGranularApprovalPolicy{MCPElicitations: true}) {
		t.Fatalf("generated approval policy = %#v", native.ApprovalPolicy)
	}
	if server.DefaultToolsApprovalMode != "prompt" || server.EnvHTTPHeaders["Authorization"] != "KAGENT_CODEX_MCP_CREDENTIAL_ABC" || server.HTTPHeaders["X-Tenant"] != "test" || len(server.EnabledTools) != 1 || server.EnabledTools[0] != "read" {
		t.Fatalf("generated MCP server configuration = %#v", server)
	}
	if agent.Description != "Review carefully" || agent.ConfigFile != agentPath {
		t.Fatalf("generated agent registration = %#v", agent)
	}
	agentContents, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	var agentConfig nativeAgentConfig
	if err := toml.Unmarshal(agentContents, &agentConfig); err != nil {
		t.Fatalf("decode generated Codex agent configuration: %v", err)
	}
	if agentConfig.Model != "gpt-5.2-codex" || agentConfig.DeveloperInstructions != `Inspect "all" changes` {
		t.Fatalf("generated Codex agent configuration = %#v", agentConfig)
	}
}

func TestNewProviderRetriesAreOptIn(t *testing.T) {
	for _, provider := range []config.Provider{
		{Name: "openai"},
		{Name: "openai", BaseURL: "https://gateway.example.com/v1"},
		{Name: "amazon-bedrock"},
	} {
		t.Run(provider.Name+provider.BaseURL, func(t *testing.T) {
			for _, test := range []struct {
				name           string
				environment    []string
				disableRetries bool
			}{
				{name: "unset"},
				{name: "empty", environment: []string{env.TestCodexDisableRetries + "="}},
				{name: "false", environment: []string{env.TestCodexDisableRetries + "=false"}},
				{name: "true", environment: []string{env.TestCodexDisableRetries + "=true"}, disableRetries: true},
			} {
				t.Run(test.name, func(t *testing.T) {
					cfg := config.Production("test-model", "help")
					cfg.Provider = provider
					raw, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					durable := t.TempDir()
					if _, err := New(t.Context(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable, Environment: test.environment}); err != nil {
						t.Fatal(err)
					}
					contents, err := os.ReadFile(filepath.Join(durable, "codex", "config.toml"))
					if err != nil {
						t.Fatal(err)
					}
					// Pointers distinguish explicit zero budgets from omitted native defaults.
					var native struct {
						Providers map[string]struct {
							Request *uint `toml:"request_max_retries"`
							Stream  *uint `toml:"stream_max_retries"`
						} `toml:"model_providers"`
					}
					if err := toml.Unmarshal(contents, &native); err != nil {
						t.Fatal(err)
					}
					if provider.Name == "amazon-bedrock" {
						if len(native.Providers) != 0 {
							t.Fatal("built-in Bedrock rejects provider retry overrides")
						}
						return
					}
					retries := native.Providers["kagent-openai"]
					if !test.disableRetries {
						if retries.Request != nil || retries.Stream != nil {
							t.Fatalf("production must omit retry budgets to preserve native defaults: %s", contents)
						}
					} else if retries.Request == nil || retries.Stream == nil || *retries.Request != 0 || *retries.Stream != 0 {
						t.Fatalf("provider retry budgets must be explicitly zero: %s", contents)
					}
				})
			}
		})
	}
}

func TestNativeTelemetryHonorsProcessOverrides(t *testing.T) {
	for _, tc := range []struct {
		name        string
		environment []string
		endpoint    string
	}{
		{"generic", []string{"OTEL_EXPORTER_OTLP_ENDPOINT=http://override:4318", "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf"}, "http://override:4318/v1/traces"},
		{"specific", []string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://override:4318/custom", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf"}, "http://override:4318/custom"},
		{"disabled SDK", []string{"OTEL_SDK_DISABLED=true"}, ""},
		{"disabled signal", []string{"OTEL_TRACES_EXPORTER=none"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Production("gpt-5.2-codex", "test")
			cfg.Provider = config.Provider{Name: "openai"}
			cfg.Telemetry = &config.Telemetry{CaptureContent: true, Traces: &config.OTLPExporter{Endpoint: "http://file:4317", Protocol: "grpc"}}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			environment := append(tc.environment, "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=NO_CONTENT")
			if _, err := New(t.Context(), Input{ConfigJSON: raw, Workspace: filepath.Join(dir, "workspace"), DurableDir: dir, Environment: environment}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "codex/config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			var native nativeConfig
			if err := toml.Unmarshal(data, &native); err != nil {
				t.Fatal(err)
			}
			if native.Otel.LogUserPrompt {
				t.Fatal("process override did not disable native content capture")
			}
			if tc.endpoint == "" {
				if native.Otel.TraceExporter != nil {
					t.Fatal("disabled process exports native traces")
				}
			} else if native.Otel.TraceExporter == nil || native.Otel.TraceExporter.OTLPHTTP == nil || native.Otel.TraceExporter.OTLPHTTP.Endpoint != tc.endpoint {
				t.Fatalf("native OTLP destination does not use process override: %#v", native.Otel.TraceExporter)
			}
		})
	}
}

func TestRenderConfigOmitsDisabledSignalExporter(t *testing.T) {
	tests := []struct {
		name       string
		telemetry  *config.Telemetry
		wantLogs   bool
		wantTraces bool
	}{
		{
			name: "logs only",
			telemetry: &config.Telemetry{
				Logs: &config.OTLPExporter{Endpoint: "http://logs:4317", Protocol: "grpc"},
			},
			wantLogs: true,
		},
		{
			name: "traces only",
			telemetry: &config.Telemetry{
				Traces: &config.OTLPExporter{Endpoint: "http://traces:4318/v1/traces", Protocol: "http/protobuf"},
			},
			wantTraces: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Production("gpt-5.2-codex", "work carefully")
			cfg.Provider = config.Provider{Name: "openai"}
			cfg.Telemetry = test.telemetry
			contents, err := renderConfig(cfg, t.TempDir(), false)
			if err != nil {
				t.Fatal(err)
			}
			var native nativeConfig
			if err := toml.Unmarshal(contents, &native); err != nil {
				t.Fatal(err)
			}
			if native.Otel == nil || (native.Otel.Exporter != nil) != test.wantLogs || (native.Otel.TraceExporter != nil) != test.wantTraces {
				t.Fatalf("generated OTEL configuration = %#v", native.Otel)
			}
		})
	}
}

func TestNewRejectsSymlinkedCodexHome(t *testing.T) {
	durable := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(durable, "codex")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Production("model", "instruction")
	cfg.Provider = config.Provider{Name: "openai"}
	raw, _ := json.Marshal(cfg)
	if _, err := New(context.Background(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable}); err == nil {
		t.Fatal("New() accepted symlinked Codex home")
	}
}

func TestPinnedCodexAcceptsGeneratedConfiguration(t *testing.T) {
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("pinned Codex CLI is not installed")
	}
	tests := []struct {
		name     string
		provider config.Provider
	}{
		{name: "OpenAI", provider: config.Provider{Name: "openai"}},
		{name: "OpenAI gateway", provider: config.Provider{Name: "openai", BaseURL: "https://gateway.example.com/v1"}},
		{name: "Bedrock", provider: config.Provider{Name: "amazon-bedrock"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertPinnedCodexAcceptsConfig(t, executable, test.provider)
		})
	}
}

func assertPinnedCodexAcceptsConfig(t *testing.T, executable string, provider config.Provider) {
	t.Helper()
	durable := filepath.Join(t.TempDir(), "data")
	cfg := config.Production("gpt-5.2-codex", "work carefully")
	cfg.Provider = provider
	cfg.Telemetry = &config.Telemetry{
		CaptureContent: true,
		Traces:         &config.OTLPExporter{Endpoint: "http://collector:4318/v1/traces", Protocol: "http/protobuf"},
		Logs:           &config.OTLPExporter{Endpoint: "http://collector:4318/v1/logs", Protocol: "http/protobuf"},
	}
	cfg.Agents = map[string]config.Agent{"reviewer": {Description: "Reviews", Instruction: "Review", Model: "gpt-5.2-codex"}}
	cfg.MCPServers = map[string]config.MCPServer{"tools": {URL: "https://mcp.example.com/mcp", EnabledTools: []string{"read"}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(context.Background(), Input{ConfigJSON: raw, Workspace: filepath.Join(durable, "workspace"), DurableDir: durable, Environment: os.Environ()}); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(durable, "codex")
	environment := append(os.Environ(), "CODEX_HOME="+codexHome, "OPENAI_API_KEY=test", "AWS_REGION=us-east-1", "AWS_BEARER_TOKEN_BEDROCK=test")
	featuresCommand := exec.Command(executable, "features", "list")
	featuresCommand.Env = environment
	featuresOutput, err := featuresCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("list features from generated Codex config: %v: %s", err, featuresOutput)
	}
	if !featureEnabled(featuresOutput, "default_mode_request_user_input") {
		t.Fatalf("default-mode request_user_input feature is not enabled:\n%s", featuresOutput)
	}
	if featureEnabled(featuresOutput, "unbounded_connection_retries") {
		t.Fatalf("unbounded connection retries remain enabled:\n%s", featuresOutput)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "app-server", "--strict-config", "--stdio")
	command.Env = environment
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"kagent-test","version":"1"}}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("initialize generated Codex config: %v: %s", err, stderr.String())
	}
	var response struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 1 || len(response.Error) != 0 || len(response.Result) == 0 {
		t.Fatalf("initialize response = %s, stderr = %s", line, stderr.String())
	}
	_ = stdin.Close()
	_ = command.Process.Kill()
	_ = command.Wait()
}

func featureEnabled(output []byte, name string) bool {
	for line := range strings.Lines(string(output)) {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == name && fields[len(fields)-1] == "true" {
			return true
		}
	}
	return false
}

func TestNativeEnvironmentDropsCompressionCodexCannotLoad(t *testing.T) {
	got := nativeEnvironment([]string{"PATH=/bin", "OTEL_EXPORTER_OTLP_COMPRESSION=gzip", "CODEX_HOME=/wrong"}, "/data/codex",
		tracing.RuntimeTelemetry{Runtime: tracing.RuntimeCodex, AgentName: "demo-codex", AgentNamespace: "team"})
	for _, variable := range got {
		if strings.HasPrefix(variable, "OTEL_EXPORTER_OTLP_COMPRESSION=") {
			t.Fatalf("environment = %v, want no OTLP compression", got)
		}
	}
	if !slices.Contains(got, "CODEX_HOME=/data/codex") || !slices.ContainsFunc(got, func(variable string) bool {
		return strings.HasPrefix(variable, "OTEL_RESOURCE_ATTRIBUTES=") && strings.Contains(variable, "gen_ai.main_agent.name=demo-codex")
	}) {
		t.Fatalf("environment = %v, want CODEX_HOME and the compiled identity", got)
	}
}
