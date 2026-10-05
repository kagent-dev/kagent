// Package adapter constructs the Claude runtime from compiler-owned
// configuration and Actor-owned paths.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/claude/internal/driver"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const approvalMCPServerName = "kagent_hitl"

// claudeSettings is the permission rules the adapter renders. A harness that
// runs as root renders them as Claude Code's managed settings, where only
// managed permission rules apply.
type claudeSettings struct {
	Permissions                     claudePermissions `json:"permissions"`
	AllowManagedPermissionRulesOnly bool              `json:"allowManagedPermissionRulesOnly,omitempty"`
}

type claudePermissions struct {
	Ask []string `json:"ask,omitempty"`
}

// Input contains compiler output and Actor-owned locations used to construct
// the Claude driver.
type Input struct {
	ConfigJSON   []byte
	Workspace    string
	DurableDir   string
	EphemeralDir string
	Environment  []string
	// PolicyDir holds what a root harness writes for Claude Code to read but
	// not change: the MCP configuration and the Google credentials.
	PolicyDir string
	// ManagedSettingsPath is Claude Code's managed settings file, where a root
	// harness renders the permission rules.
	ManagedSettingsPath string
}

// New validates and materializes Claude-owned state, then constructs its driver.
func New(ctx context.Context, input Input) (*driver.ProcessDriver, error) {
	cfg, err := config.Parse(input.ConfigJSON)
	if err != nil {
		return nil, err
	}
	agentsJSON, err := cfg.AgentsJSON()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(input.Workspace) || !filepath.IsAbs(input.DurableDir) || !filepath.IsAbs(input.EphemeralDir) {
		return nil, fmt.Errorf("workspace, durable, and ephemeral directories must be absolute paths")
	}
	claudeDir := filepath.Join(input.DurableDir, "claude")
	var skillRoot string
	if cfg.SkillResources != nil {
		skillRoot = filepath.Join(input.DurableDir, "generated", "claude")
	}
	holdsCallerCredential := propagateCallerToken(input.Environment) && len(cfg.MCPServers) != 0
	if holdsCallerCredential {
		if err := validateForwardedServers(cfg.MCPServers); err != nil {
			return nil, err
		}
	}
	runsAsRoot := os.Geteuid() == 0
	if runsAsRoot && (!filepath.IsAbs(input.PolicyDir) || !filepath.IsAbs(input.ManagedSettingsPath)) {
		return nil, fmt.Errorf("a harness that runs as root needs absolute policy and managed settings paths")
	}
	if holdsCallerCredential {
		if !runsAsRoot {
			// Claude Code would run as the harness's own user and could read the
			// caller's credential out of the harness process.
			return nil, fmt.Errorf("%s requires the harness to run as root, so that Claude Code runs as another user", config.PropagateTokenEnvName)
		}
	}
	files := harnessFiles{dir: input.EphemeralDir}
	claudeTrees := []string{input.Workspace, claudeDir}
	if runsAsRoot {
		files = harnessFiles{dir: input.PolicyDir, groupReadable: true}
	} else {
		claudeTrees = append(claudeTrees, input.EphemeralDir)
	}
	if skillRoot != "" {
		claudeTrees = append(claudeTrees, skillRoot)
	}
	if runsAsRoot {
		for _, tree := range claudeTrees {
			if err := utils.ReclaimTree(tree); err != nil {
				return nil, fmt.Errorf("reclaim %s from the unprivileged user: %w", tree, err)
			}
		}
	}
	for _, directory := range []struct{ name, path string }{
		{name: "workspace", path: input.Workspace},
		{name: "Claude state", path: claudeDir},
	} {
		if err := utils.EnsurePrivateDir(directory.path); err != nil {
			return nil, fmt.Errorf("prepare %s directory: %w", directory.name, err)
		}
	}
	var pluginDirs []string
	if cfg.SkillResources != nil {
		skillsDir := filepath.Join(skillRoot, ".claude", "skills")
		if err := utils.EnsurePrivateDir(skillsDir); err != nil {
			return nil, fmt.Errorf("prepare generated Claude skills directory: %w", err)
		}
		materialized, err := agentplugins.Materialize(ctx, *cfg.SkillResources, agentplugins.Paths{
			Packages: filepath.Join(claudeDir, "packages"),
			Skills:   skillsDir,
		})
		if err != nil {
			return nil, fmt.Errorf("materialize Claude skills: %w", err)
		}
		pluginDirs = materialized.ClaudeFormatPluginRoots()
	}
	environment := setEnvironment(input.Environment, config.ClaudeConfigDirEnvName, claudeDir)
	// The native runtime inherits the compiled identity through the standard
	// resource variable, so no user-supplied marker is required.
	environment, awaitTelemetry := nativeTelemetryEnvironment(telemetry.WithDefaults(tracing.ResourceEnvironment(environment, cfg.RuntimeTelemetry.ChildResource())), cfg.RuntimeTelemetry)
	// The image and compiler pin an exact Claude version. Prevent both automatic
	// and manual update paths from changing that runtime after validation.
	environment = setEnvironment(environment, config.DisableUpdatesEnvName, "1")
	environment, err = materializeGoogleCredentials(environment, files)
	if err != nil {
		return nil, err
	}
	if _, exists := cfg.MCPServers[approvalMCPServerName]; exists {
		return nil, fmt.Errorf("Claude MCP server name %q is reserved for human approval", approvalMCPServerName)
	}
	var forwarder *driver.CredentialForwarder
	var approvalBroker *driver.ApprovalBroker
	closeListeners := func() {
		if forwarder != nil {
			_ = forwarder.Close()
		}
		if approvalBroker != nil {
			_ = approvalBroker.Close()
		}
	}
	if holdsCallerCredential {
		forwarder, err = frontMCPServers(&cfg)
		if err != nil {
			return nil, err
		}
	}
	protectedServers := approvalServerNames(cfg.MCPServers)
	settings := claudeSettings{AllowManagedPermissionRulesOnly: runsAsRoot}
	var permissionPromptTool string
	if len(protectedServers) != 0 {
		approvalBroker, err = driver.NewApprovalBroker(protectedServers, cfg.MaxEventBytes)
		if err != nil {
			closeListeners()
			return nil, fmt.Errorf("start Claude approval broker: %w", err)
		}
		settings.Permissions.Ask = approvalBroker.AskRules()
		permissionPromptTool = "mcp__" + approvalMCPServerName + "__" + driver.ApprovalToolName
		mcpServers := make(map[string]config.MCPServer, len(cfg.MCPServers)+1)
		maps.Copy(mcpServers, cfg.MCPServers)
		mcpServers[approvalMCPServerName] = config.MCPServer{
			Type: "http", URL: approvalBroker.URL(), Headers: approvalBroker.Headers(),
		}
		cfg.MCPServers = mcpServers
	}
	var settingsPath string
	if runsAsRoot || len(settings.Permissions.Ask) != 0 {
		settingsJSON, err := json.Marshal(settings)
		if err != nil {
			closeListeners()
			return nil, fmt.Errorf("encode Claude settings: %w", err)
		}
		if runsAsRoot {
			err = utils.ReplaceGroupReadableFile(input.ManagedSettingsPath, settingsJSON, config.UnprivilegedGID)
		} else {
			settingsPath, err = files.write("settings.json", settingsJSON)
		}
		if err != nil {
			closeListeners()
			return nil, fmt.Errorf("materialize Claude settings: %w", err)
		}
	}
	mcpJSON, err := cfg.MCPConfigJSON()
	if err != nil {
		closeListeners()
		return nil, err
	}
	var mcpConfigPath string
	if len(mcpJSON) != 0 {
		if mcpConfigPath, err = files.write("mcp.json", mcpJSON); err != nil {
			closeListeners()
			return nil, fmt.Errorf("materialize Claude MCP configuration: %w", err)
		}
	}
	var runAs *driver.Identity
	if runsAsRoot {
		if err := handOver(input.DurableDir, claudeTrees); err != nil {
			closeListeners()
			return nil, err
		}
		runAs = &driver.Identity{UID: config.UnprivilegedUID, GID: config.UnprivilegedGID}
		environment = setEnvironment(environment, config.HomeEnvName, claudeDir)
	}
	processConfig := driver.ProcessConfig{
		Executable: cfg.ClaudeExecutable, ExpectedVersion: cfg.ExpectedClaudeVersion,
		StrictVersion: cfg.StrictVersion, Workspace: input.Workspace, Model: cfg.Model,
		AppendSystemPrompt: cfg.AppendSystemPrompt, AgentsJSON: agentsJSON, MCPConfigPath: mcpConfigPath,
		SettingsPath: settingsPath, PermissionPromptTool: permissionPromptTool, ApprovalBroker: approvalBroker,
		SkillRoot: skillRoot, PluginDirs: pluginDirs, Environment: environment,
		MaxEventBytes: cfg.MaxEventBytes, MaxStderrBytes: cfg.MaxStderrBytes,
		InterruptGrace: cfg.InterruptGrace(), AwaitTelemetry: awaitTelemetry, RunAs: runAs,
	}
	if forwarder != nil {
		processConfig.CallerCredentials = forwarder
	}
	return driver.NewProcessDriver(processConfig), nil
}

// propagateCallerToken reports whether the Actor environment asks for the
// caller's credential on MCP calls, the same switch the Go ADK reads.
func propagateCallerToken(environment []string) bool {
	return strings.EqualFold(strings.TrimSpace(environmentValue(environment, config.PropagateTokenEnvName)), "true")
}

// frontMCPServers starts the credential forwarder for the compiled MCP servers
// and rewrites them to their loopback endpoints, so the written configuration
// carries no upstream URL, no static header and no credential.
func frontMCPServers(cfg *config.Config) (*driver.CredentialForwarder, error) {
	upstream := make(map[string]driver.UpstreamMCPServer, len(cfg.MCPServers))
	for name, server := range cfg.MCPServers {
		upstream[name] = driver.UpstreamMCPServer{URL: server.URL, Headers: server.Headers}
	}
	forwarder, err := driver.NewCredentialForwarder(upstream, cfg.MaxEventBytes)
	if err != nil {
		return nil, fmt.Errorf("start Claude credential forwarder: %w", err)
	}
	fronted := make(map[string]config.MCPServer, len(cfg.MCPServers))
	for name, server := range cfg.MCPServers {
		fronted[name] = config.MCPServer{
			Type: "http", URL: forwarder.URL(name), Headers: forwarder.Headers(),
			RequireApproval: server.RequireApproval,
		}
	}
	cfg.MCPServers = fronted
	return forwarder, nil
}

// validateForwardedServers refuses the servers the credential forwarder cannot
// front.
func validateForwardedServers(servers map[string]config.MCPServer) error {
	for name, server := range servers {
		if server.Type != "http" {
			return fmt.Errorf("caller credential forwarding requires streamable HTTP MCP servers; %q uses %q", name, server.Type)
		}
	}
	return nil
}

// harnessFiles is where the adapter writes the files Claude Code reads. A root
// harness writes them group-readable for the unprivileged user, which can read
// but neither edit nor replace them; otherwise they are owner-only.
type harnessFiles struct {
	dir           string
	groupReadable bool
}

func (f harnessFiles) write(name string, contents []byte) (string, error) {
	path := filepath.Join(f.dir, name)
	if f.groupReadable {
		return path, utils.ReplaceGroupReadableFile(path, contents, config.UnprivilegedGID)
	}
	return path, utils.ReplacePrivateFile(path, contents)
}

func environmentValue(environment []string, name string) string {
	for _, entry := range environment {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value
		}
	}
	return ""
}

func approvalServerNames(servers map[string]config.MCPServer) (protected []string) {
	for name, server := range servers {
		if server.RequireApproval {
			protected = append(protected, name)
		}
	}
	return protected
}

func materializeGoogleCredentials(environment []string, files harnessFiles) ([]string, error) {
	// The compiler injects the Secret value as JSON, while Google ADC expects a
	// file path. Keep the credential out of the well-known path under /data,
	// which is durable and may be snapshotted.
	prefix := config.GoogleCredentialsJSONEnvName + "="
	var credentials string
	filtered := make([]string, 0, len(environment))
	for _, item := range environment {
		if strings.HasPrefix(item, prefix) {
			if credentials != "" {
				return nil, fmt.Errorf("%s is configured more than once", config.GoogleCredentialsJSONEnvName)
			}
			credentials = strings.TrimPrefix(item, prefix)
			continue
		}
		filtered = append(filtered, item)
	}
	if credentials == "" {
		return filtered, nil
	}
	if !json.Valid([]byte(credentials)) {
		return nil, fmt.Errorf("%s must contain valid JSON", config.GoogleCredentialsJSONEnvName)
	}
	path, err := files.write("google-credentials.json", []byte(credentials))
	if err != nil {
		return nil, fmt.Errorf("materialize Google credentials: %w", err)
	}
	return setEnvironment(filtered, config.GoogleApplicationCredentialsEnvName, path), nil
}

// handOver gives Claude's trees to the image's unprivileged user, which Claude
// Code runs as when the harness runs as root, so the harness process and the
// turn's credential in its memory are out of reach of Claude and its tools.
// The directories above a tree, up to the durable directory, stay the
// harness's and only gain search permission: Claude can reach its trees but
// cannot replace the harness's own state beside them.
func handOver(durableDir string, trees []string) error {
	for _, tree := range trees {
		if err := utils.ChownTree(tree, config.UnprivilegedUID, config.UnprivilegedGID); err != nil {
			return fmt.Errorf("hand %s to the unprivileged user: %w", tree, err)
		}
		for dir := filepath.Dir(tree); strings.HasPrefix(dir, durableDir); dir = filepath.Dir(dir) {
			if err := searchableByAll(dir); err != nil {
				return fmt.Errorf("let the unprivileged user traverse %s: %w", dir, err)
			}
			if dir == durableDir {
				break
			}
		}
	}
	return nil
}

func searchableByAll(dir string) error {
	if err := os.Lchown(dir, 0, 0); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return os.Chmod(dir, info.Mode().Perm()|0o111)
}

// nativeTelemetryEnvironment turns on Claude Code telemetry for the signals the
// controller exports, and its content flags when capture is on. It reports
// whether telemetry is on.
func nativeTelemetryEnvironment(environment []string, telemetry tracing.RuntimeTelemetry) ([]string, bool) {
	exported := func(name string) bool {
		return slices.Contains(environment, name+"=otlp")
	}
	traces, logs := exported("OTEL_TRACES_EXPORTER"), exported("OTEL_LOGS_EXPORTER")
	if !traces && !logs && !exported("OTEL_METRICS_EXPORTER") {
		return environment, false
	}
	flags := []string{"CLAUDE_CODE_ENABLE_TELEMETRY", "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"}
	if telemetry.CaptureContent {
		flags = append(flags, "OTEL_LOG_USER_PROMPTS", "OTEL_LOG_TOOL_DETAILS")
		if traces {
			flags = append(flags, "OTEL_LOG_TOOL_CONTENT")
		}
		if logs {
			flags = append(flags, "OTEL_LOG_ASSISTANT_RESPONSES")
		}
	}
	for _, flag := range flags {
		environment = setEnvironment(environment, flag, "1")
	}
	return environment, true
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
