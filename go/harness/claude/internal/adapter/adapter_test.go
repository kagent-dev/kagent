package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"github.com/stretchr/testify/require"
)

func TestNewMaterializesDurableDirectories(t *testing.T) {
	skipAsRoot(t)
	durableDir := filepath.Join(t.TempDir(), "data")
	ephemeralDir := filepath.Join(t.TempDir(), "credentials")
	workspace := filepath.Join(durableDir, "workspace")
	runner, err := New(context.Background(), Input{
		ConfigJSON: []byte(`{"version":7,"claude_executable":"claude","expected_claude_version":"2.1.285","strict_version":true,"max_event_bytes":100,"max_stderr_bytes":100,"interrupt_grace_millis":100}`),
		Workspace:  workspace,
		DurableDir: durableDir, EphemeralDir: ephemeralDir,
		Environment: []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/wrong", "DISABLE_UPDATES=0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner == nil {
		t.Fatal("New() returned a nil runner")
	}
	for _, path := range []string{workspace, filepath.Join(durableDir, "claude")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s permissions = %o, want 700", path, info.Mode().Perm())
		}
	}
}

func TestNewMaterializesSkillsAndMCPConfig(t *testing.T) {
	skipAsRoot(t)
	durableDir := filepath.Join(t.TempDir(), "data")
	skillRoot := filepath.Join(durableDir, "generated", "claude")
	packageRoot := filepath.Join(durableDir, "generated", "packages", "standalone-0")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "SKILL.md"), []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.SkillResources = &agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "review", Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: "unused", Commit: strings.Repeat("a", 40)}},
	}}}
	cfg.MCPServers = map[string]config.MCPServer{"tools": {Type: "http", URL: "https://mcp.example.com/mcp"}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ephemeralDir := filepath.Join(t.TempDir(), "generated")
	runner, err := New(context.Background(), Input{
		ConfigJSON: raw, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: ephemeralDir, Environment: []string{"PATH=/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(skillRoot, ".claude", "skills", "review", "SKILL.md"): "# Review",
		filepath.Join(ephemeralDir, "mcp.json"):                             `{"mcpServers":{"tools":{"type":"http","url":"https://mcp.example.com/mcp"}}}`,
	} {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != want {
			t.Fatalf("%s = %q, %v; want %q", path, contents, err, want)
		}
	}
	if args := strings.Join(runner.Args(runtime.Turn{Prompt: "test"}), "\n"); !strings.Contains(args, "--add-dir\n"+skillRoot) {
		t.Fatalf("arguments do not expose materialized skills: %s", args)
	}
}

func TestNewMaterializesApprovalSettings(t *testing.T) {
	skipAsRoot(t)
	durableDir := filepath.Join(t.TempDir(), "data")
	ephemeralDir := filepath.Join(t.TempDir(), "generated")
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.MCPServers = map[string]config.MCPServer{
		"protected": {Type: "http", URL: "https://mcp.example.com/write", RequireApproval: true},
		"readonly":  {Type: "http", URL: "https://mcp.example.com/read"},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(context.Background(), Input{
		ConfigJSON: raw, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: ephemeralDir, Environment: []string{"PATH=/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	settings, err := os.ReadFile(filepath.Join(ephemeralDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(settings), `{"permissions":{"ask":["mcp__protected__*"]}}`; got != want {
		t.Fatalf("approval settings = %s, want %s", got, want)
	}
	mcpConfig, err := os.ReadFile(filepath.Join(ephemeralDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var materialized struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpConfig, &materialized); err != nil {
		t.Fatal(err)
	}
	approvalServer := materialized.Servers[approvalMCPServerName]
	if approvalServer.Type != "http" || !strings.HasPrefix(approvalServer.URL, "http://127.0.0.1:") ||
		!strings.HasPrefix(approvalServer.Headers["Authorization"], "Bearer ") {
		t.Fatalf("private approval MCP server = %#v", approvalServer)
	}
	if len(materialized.Servers) != 3 {
		t.Fatalf("materialized MCP servers = %#v", materialized.Servers)
	}
	args := strings.Join(runner.Args(runtime.Turn{Prompt: "test"}), "\n")
	for _, required := range []string{
		"--settings\n" + filepath.Join(ephemeralDir, "settings.json"),
		"--dangerously-skip-permissions",
		"--permission-prompt-tool\nmcp__" + approvalMCPServerName + "__approve",
	} {
		if !strings.Contains(args, required) {
			t.Fatalf("approval arguments do not contain %q: %s", required, args)
		}
	}
	for _, forbidden := range []string{"--bare", "--permission-mode", "dontAsk", "--setting-sources"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("approval arguments contain %q: %s", forbidden, args)
		}
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	input := Input{ConfigJSON: []byte(`{}`), Workspace: "relative", DurableDir: "relative", EphemeralDir: "relative"}
	if _, err := New(context.Background(), input); err == nil {
		t.Fatal("New() accepted invalid input")
	}
}

func TestSetEnvironmentOverridesExistingValue(t *testing.T) {
	got := setEnvironment([]string{"A=1", "A=2", "B=3"}, "A", "4")
	want := []string{"B=3", "A=4"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("setEnvironment() = %v, want %v", got, want)
	}
}

func TestNativeTelemetryEnvironmentFollowsExportedSignals(t *testing.T) {
	for name, test := range map[string]struct {
		environment []string
		capture     bool
		want        []string
		absent      []string
	}{
		"off": {
			environment: []string{"OTEL_TRACES_EXPORTER=none", "OTEL_METRICS_EXPORTER=none", "OTEL_LOGS_EXPORTER=none"},
			capture:     true,
			absent:      []string{"CLAUDE_CODE_ENABLE_TELEMETRY", "OTEL_LOG_USER_PROMPTS"},
		},
		"traces without capture": {
			environment: []string{"OTEL_TRACES_EXPORTER=otlp", "OTEL_LOGS_EXPORTER=none"},
			want:        []string{"CLAUDE_CODE_ENABLE_TELEMETRY", "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"},
			absent:      []string{"OTEL_LOG_USER_PROMPTS", "OTEL_LOG_TOOL_CONTENT"},
		},
		"traces with capture": {
			environment: []string{"OTEL_TRACES_EXPORTER=otlp", "OTEL_LOGS_EXPORTER=none"},
			capture:     true,
			want:        []string{"OTEL_LOG_USER_PROMPTS", "OTEL_LOG_TOOL_DETAILS", "OTEL_LOG_TOOL_CONTENT"},
			absent:      []string{"OTEL_LOG_ASSISTANT_RESPONSES"},
		},
		"logs with capture": {
			environment: []string{"OTEL_TRACES_EXPORTER=none", "OTEL_LOGS_EXPORTER=otlp"},
			capture:     true,
			want:        []string{"OTEL_LOG_USER_PROMPTS", "OTEL_LOG_TOOL_DETAILS", "OTEL_LOG_ASSISTANT_RESPONSES"},
			absent:      []string{"OTEL_LOG_TOOL_CONTENT"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, enabled := nativeTelemetryEnvironment(test.environment, tracing.RuntimeTelemetry{CaptureContent: test.capture})
			if enabled != slices.Contains(got, "CLAUDE_CODE_ENABLE_TELEMETRY=1") {
				t.Errorf("enabled = %t for %v", enabled, got)
			}
			for _, flag := range test.want {
				if !slices.Contains(got, flag+"=1") {
					t.Errorf("%s missing from %v", flag, got)
				}
			}
			for _, flag := range test.absent {
				if slices.ContainsFunc(got, func(variable string) bool { return strings.HasPrefix(variable, flag+"=") }) {
					t.Errorf("%s set in %v", flag, got)
				}
			}
		})
	}
}

func TestNewRefusesTheCallerTokenUnlessClaudeRunsAsAnotherUser(t *testing.T) {
	skipAsRoot(t)
	durableDir := filepath.Join(t.TempDir(), "data")
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.MCPServers = map[string]config.MCPServer{"tools": {Type: "http", URL: "https://tools.example.com/mcp"}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	_, err = New(t.Context(), Input{
		ConfigJSON: raw, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: filepath.Join(t.TempDir(), "generated"), Environment: []string{"PATH=/bin", "KAGENT_PROPAGATE_TOKEN=true"},
	})
	require.ErrorContains(t, err, "KAGENT_PROPAGATE_TOKEN requires the harness to run as root")
}

func TestNewRefusesToFrontSSEServers(t *testing.T) {
	durableDir := filepath.Join(t.TempDir(), "data")
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	cfg.MCPServers = map[string]config.MCPServer{"legacy": {Type: "sse", URL: "https://mcp.example.com/sse"}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(context.Background(), Input{
		ConfigJSON: raw, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: filepath.Join(t.TempDir(), "generated"), Environment: []string{"KAGENT_PROPAGATE_TOKEN=true"},
	})
	if err == nil || !strings.Contains(err.Error(), "streamable HTTP") {
		t.Fatalf("New() error = %v, want the SSE refusal", err)
	}
}

func TestNewRendersNoSettingsWithoutApprovals(t *testing.T) {
	skipAsRoot(t)
	durableDir := filepath.Join(t.TempDir(), "data")
	ephemeralDir := filepath.Join(t.TempDir(), "generated")
	cfg := config.Production("claude-test", "help")
	cfg.StrictVersion = false
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	runner, err := New(t.Context(), Input{
		ConfigJSON: raw, Workspace: filepath.Join(durableDir, "workspace"), DurableDir: durableDir,
		EphemeralDir: ephemeralDir, Environment: []string{"PATH=/bin"},
	})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(ephemeralDir, "settings.json"))
	args := runner.Args(runtime.Turn{Prompt: "test"})
	require.NotContains(t, args, "--settings")
	require.NotContains(t, args, "--permission-prompt-tool")
}

// skipAsRoot skips a test of the harness that runs as the same user as Claude
// Code; adapter_unix_test.go covers the root harness.
func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a root harness writes its files for the unprivileged user")
	}
}

func TestResolveCredentialHeaders(t *testing.T) {
	credential := config.MCPCredentialEnvPrefix + "0123456789ABCDEF"
	resolved, err := resolveCredentialHeaders(map[string]string{
		"Authorization": "${" + credential + "}",
		"X-Tenant":      "${HOME}",
		"X-Literal":     "${" + config.MCPCredentialEnvPrefix,
	}, []string{"HOME=/home/kagent", credential + "=kagent-credential-injected"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"Authorization": "kagent-credential-injected",
		"X-Tenant":      "${HOME}",
		"X-Literal":     "${" + config.MCPCredentialEnvPrefix,
	}, resolved)

	_, err = resolveCredentialHeaders(map[string]string{"Authorization": "${" + credential + "}"}, []string{"HOME=/home/kagent"})
	require.ErrorContains(t, err, credential+", which the environment does not set")
}

func TestFrontMCPServersKeepsServerSettings(t *testing.T) {
	cfg := config.Production("claude-test", "help")
	cfg.MCPServers = map[string]config.MCPServer{"tools": {
		Type: "http", URL: "https://mcp.example.com/mcp", RequireApproval: true, TimeoutMillis: 180000,
	}}
	forwarder, err := frontMCPServers(&cfg, []string{"tools"}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })

	fronted := cfg.MCPServers["tools"]
	require.Equal(t, forwarder.URL("tools"), fronted.URL)
	require.True(t, fronted.RequireApproval)
	require.Equal(t, int64(180000), fronted.TimeoutMillis)
}

// A turn whose every server is on an actor-JWT host writes no loopback token:
// Claude reaches muster itself with the placeholder the gateway replaces.
func TestMCPConfigLeavesTheLoopbackTokenOutOfAnActorJWTOnlyTurn(t *testing.T) {
	cfg := config.Production("claude-test", "help")
	compiled := map[string]config.MCPServer{
		"muster": {Type: "http", URL: "https://muster.example.svc/mcp", Headers: map[string]string{"X-Muster-Toolset": "preset:read-only"}, TimeoutMillis: 180000},
	}
	cfg.MCPServers = compiled
	forwarder, err := frontMCPServers(&cfg, []string{"muster"}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })
	file := mcpConfigFile{servers: cfg.MCPServers, compiled: compiled, files: harnessFiles{dir: t.TempDir()}}

	require.JSONEq(t, `{"mcpServers":{"muster":{"type":"http","url":"https://muster.example.svc/mcp","timeout":180000,
		"headers":{"X-Muster-Toolset":"preset:read-only","Authorization":"Bearer kagent-credential-injected"}}}}`,
		readMCPConfig(t, file, []string{"muster.example.svc"}))
	require.Empty(t, compiled["muster"].Headers["Authorization"], "the compiled server is left as it was")

	forwarded := readMCPConfig(t, file, nil)
	require.Contains(t, forwarded, forwarder.URL("muster"), "a turn that forwards the caller's credential goes through the forwarder")
	require.Contains(t, forwarded, forwarder.Headers()["Authorization"])
}

// Without the caller's credential no server is fronted, and a server on an
// actor-JWT host still needs the placeholder: the gateway replaces only an
// Authorization the request carries.
func TestMCPConfigSendsThePlaceholderWithoutTheForwarder(t *testing.T) {
	compiled := map[string]config.MCPServer{
		"muster": {Type: "http", URL: "https://muster.example.svc/mcp"},
		"tools":  {Type: "http", URL: "https://tools.example.svc/mcp"},
	}
	file := mcpConfigFile{servers: compiled, compiled: compiled, files: harnessFiles{dir: t.TempDir()}}

	require.JSONEq(t, `{"mcpServers":{
		"muster":{"type":"http","url":"https://muster.example.svc/mcp","headers":{"Authorization":"Bearer kagent-credential-injected"}},
		"tools":{"type":"http","url":"https://tools.example.svc/mcp"}}}`,
		readMCPConfig(t, file, []string{"muster.example.svc"}))
	require.JSONEq(t, `{"mcpServers":{
		"muster":{"type":"http","url":"https://muster.example.svc/mcp"},
		"tools":{"type":"http","url":"https://tools.example.svc/mcp"}}}`,
		readMCPConfig(t, file, nil))
}

// The forwarder enforces a tool selection, so such a server stays on loopback
// on an actor-JWT host too.
func TestMCPConfigKeepsASelectionOnTheForwarder(t *testing.T) {
	cfg := config.Production("claude-test", "help")
	compiled := map[string]config.MCPServer{
		"muster": {Type: "http", URL: "https://muster.example.svc/mcp", Tools: []string{"list"}},
	}
	cfg.MCPServers = compiled
	forwarder, err := frontMCPServers(&cfg, []string{"muster"}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })
	file := mcpConfigFile{servers: cfg.MCPServers, compiled: compiled, files: harnessFiles{dir: t.TempDir()}}

	written := readMCPConfig(t, file, []string{"muster.example.svc"})
	require.Contains(t, written, forwarder.URL("muster"))
	require.NotContains(t, written, "muster.example.svc")
}

func readMCPConfig(t *testing.T, file mcpConfigFile, actorJWTHosts []string) string {
	t.Helper()
	require.NoError(t, file.write(actorJWTHosts))
	contents, err := os.ReadFile(file.files.path("mcp.json"))
	require.NoError(t, err)
	return string(contents)
}

func TestFrontedServersSelectToolsWithoutCallerCredential(t *testing.T) {
	servers := map[string]config.MCPServer{
		"selected": {Type: "http", URL: "https://selected.example.com/mcp", Tools: []string{"echo"}},
		"whole":    {Type: "http", URL: "https://whole.example.com/mcp"},
	}
	require.Equal(t, []string{"selected"}, frontedServers(servers, false))
	require.Equal(t, []string{"selected", "whole"}, frontedServers(servers, true))
	require.Empty(t, frontedServers(map[string]config.MCPServer{"whole": servers["whole"]}, false))

	cfg := config.Production("claude-test", "help")
	cfg.MCPServers = servers
	forwarder, err := frontMCPServers(&cfg, frontedServers(servers, false), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })
	require.Equal(t, forwarder.URL("selected"), cfg.MCPServers["selected"].URL)
	require.Empty(t, cfg.MCPServers["selected"].Tools, "Claude's MCP file carries no selection; the forwarder enforces it")
	require.Equal(t, servers["whole"], cfg.MCPServers["whole"])
}

// Without the caller's credential the forwarder still fronts a server with a
// tool selection, and sends the placeholder to it on an actor-JWT host.
func TestActorJWTHostsOnlyBindsNoCallerCredential(t *testing.T) {
	var authorizations []string
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	port := strings.TrimPrefix(upstream.URL, "http://127.0.0.1")
	cfg := config.Production("claude-test", "help")
	cfg.MCPServers = map[string]config.MCPServer{
		"muster": {Type: "http", URL: "http://localhost" + port + "/mcp", Tools: []string{"list"}},
		"tools":  {Type: "http", URL: upstream.URL + "/mcp", Tools: []string{"echo"}},
	}
	forwarder, err := frontMCPServers(&cfg, frontedServers(cfg.MCPServers, false), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })
	binder := actorJWTHostsOnly{forwarder}

	require.False(t, binder.TakesCallerCredential(nil), "no turn is bounded by a credential it does not hold")
	binder.Bind("Bearer person-token", []string{"localhost"})
	for _, name := range []string{"muster", "tools"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, forwarder.URL(name), strings.NewReader(`{"jsonrpc":"2.0"}`))
		require.NoError(t, err)
		for header, value := range forwarder.Headers() {
			request.Header.Set(header, value)
		}
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		_ = response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
	}
	binder.Clear()
	require.Equal(t, []string{"Bearer kagent-credential-injected", ""}, authorizations)
}

func TestTurnTimeoutReadsTheHarnessEnvironment(t *testing.T) {
	for _, test := range []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "", want: 0},
		{value: "30m", want: 30 * time.Minute},
		{value: " 1h30m ", want: 90 * time.Minute},
		{value: "0s", wantErr: true},
		{value: "-5m", wantErr: true},
		{value: "thirty minutes", wantErr: true},
	} {
		got, err := turnTimeout([]string{"PATH=/bin", config.TurnTimeoutEnvName + "=" + test.value})
		if (err != nil) != test.wantErr || got != test.want {
			t.Fatalf("turnTimeout(%q) = %s, %v; want %s, error %v", test.value, got, err, test.want, test.wantErr)
		}
	}
}
