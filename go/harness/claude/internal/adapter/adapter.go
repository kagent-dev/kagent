// Package adapter constructs the Claude runtime from compiler-owned
// configuration and Actor-owned paths.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/claude/internal/driver"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
)

const approvalMCPServerName = "kagent_hitl"

// Input contains compiler output and Actor-owned locations used to construct
// the Claude driver.
type Input struct {
	ConfigJSON   []byte
	Workspace    string
	DurableDir   string
	EphemeralDir string
	Environment  []string
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
	for _, directory := range []struct{ name, path string }{
		{name: "workspace", path: input.Workspace},
		{name: "Claude state", path: claudeDir},
	} {
		if err := utils.EnsurePrivateDir(directory.path); err != nil {
			return nil, fmt.Errorf("prepare %s directory: %w", directory.name, err)
		}
	}
	if cfg.SkillResources != nil {
		skillsDir := filepath.Join(skillRoot, ".claude", "skills")
		if err := utils.EnsurePrivateDir(skillsDir); err != nil {
			return nil, fmt.Errorf("prepare generated Claude skills directory: %w", err)
		}
		if _, err := agentplugins.Materialize(ctx, *cfg.SkillResources, agentplugins.Paths{
			Packages: filepath.Join(claudeDir, "packages"),
			Skills:   skillsDir,
		}); err != nil {
			return nil, fmt.Errorf("materialize Claude skills: %w", err)
		}
	}
	environment := setEnvironment(input.Environment, config.ClaudeConfigDirEnvName, claudeDir)
	// The image and compiler pin an exact Claude version. Prevent both automatic
	// and manual update paths from changing that runtime after validation.
	environment = setEnvironment(environment, config.DisableUpdatesEnvName, "1")
	environment, err = materializeGoogleCredentials(environment, input.EphemeralDir)
	if err != nil {
		return nil, err
	}
	protectedServers := approvalServerNames(cfg.MCPServers)
	var approvalBroker *driver.ApprovalBroker
	var settingsPath string
	var permissionPromptTool string
	if len(protectedServers) != 0 {
		if _, exists := cfg.MCPServers[approvalMCPServerName]; exists {
			return nil, fmt.Errorf("Claude MCP server name %q is reserved for human approval", approvalMCPServerName)
		}
		if err := utils.EnsurePrivateDir(input.EphemeralDir); err != nil {
			return nil, fmt.Errorf("prepare ephemeral Claude settings directory: %w", err)
		}
		approvalBroker, err = driver.NewApprovalBroker(protectedServers, cfg.MaxEventBytes)
		if err != nil {
			return nil, fmt.Errorf("start Claude approval broker: %w", err)
		}
		settingsJSON, settingsErr := approvalBroker.SettingsJSON()
		if settingsErr != nil {
			_ = approvalBroker.Close()
			return nil, settingsErr
		}
		settingsPath = filepath.Join(input.EphemeralDir, "settings.json")
		if err := utils.ReplacePrivateFile(settingsPath, settingsJSON); err != nil {
			_ = approvalBroker.Close()
			return nil, fmt.Errorf("materialize Claude approval settings: %w", err)
		}
		permissionPromptTool = "mcp__" + approvalMCPServerName + "__" + driver.ApprovalToolName
		mcpServers := make(map[string]config.MCPServer, len(cfg.MCPServers)+1)
		maps.Copy(mcpServers, cfg.MCPServers)
		mcpServers[approvalMCPServerName] = config.MCPServer{
			Type: "http", URL: approvalBroker.URL(), Headers: approvalBroker.Headers(),
		}
		cfg.MCPServers = mcpServers
	}
	mcpJSON, err := cfg.MCPConfigJSON()
	if err != nil {
		if approvalBroker != nil {
			_ = approvalBroker.Close()
		}
		return nil, err
	}
	var mcpConfigPath string
	if len(mcpJSON) != 0 {
		if err := utils.EnsurePrivateDir(input.EphemeralDir); err != nil {
			if approvalBroker != nil {
				_ = approvalBroker.Close()
			}
			return nil, fmt.Errorf("prepare ephemeral MCP directory: %w", err)
		}
		mcpConfigPath = filepath.Join(input.EphemeralDir, "mcp.json")
		if err := utils.ReplacePrivateFile(mcpConfigPath, mcpJSON); err != nil {
			if approvalBroker != nil {
				_ = approvalBroker.Close()
			}
			return nil, fmt.Errorf("materialize Claude MCP configuration: %w", err)
		}
	}
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: cfg.ClaudeExecutable, ExpectedVersion: cfg.ExpectedClaudeVersion,
		StrictVersion: cfg.StrictVersion, Workspace: input.Workspace, Model: cfg.Model,
		AppendSystemPrompt: cfg.AppendSystemPrompt, AgentsJSON: agentsJSON, MCPConfigPath: mcpConfigPath,
		SettingsPath: settingsPath, PermissionPromptTool: permissionPromptTool, ApprovalBroker: approvalBroker,
		SkillRoot: skillRoot, Environment: environment,
		MaxEventBytes: cfg.MaxEventBytes, MaxStderrBytes: cfg.MaxStderrBytes,
		InterruptGrace: cfg.InterruptGrace(),
	}), nil
}

func approvalServerNames(servers map[string]config.MCPServer) (protected []string) {
	for name, server := range servers {
		if server.RequireApproval {
			protected = append(protected, name)
		}
	}
	return protected
}

func materializeGoogleCredentials(environment []string, directory string) ([]string, error) {
	// The compiler injects the Secret value as JSON, while Google ADC expects a
	// file path. Keep the credential in ephemeral Actor storage rather than the
	// well-known path under /data, which is durable and may be snapshotted.
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
	if err := utils.EnsurePrivateDir(directory); err != nil {
		return nil, fmt.Errorf("prepare ephemeral credentials directory: %w", err)
	}
	path := filepath.Join(directory, "google-credentials.json")
	if err := utils.ReplacePrivateFile(path, []byte(credentials)); err != nil {
		return nil, fmt.Errorf("materialize Google credentials: %w", err)
	}
	return setEnvironment(filtered, config.GoogleApplicationCredentialsEnvName, path), nil
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
