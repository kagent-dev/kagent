// Package adapter constructs the Claude runtime from compiler-owned
// configuration and Actor-owned paths.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/core/v2/agentplugins"
	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/claude/internal/driver"
)

const (
	claudeConfigDirEnv              = "CLAUDE_CONFIG_DIR"
	disableUpdatesEnv               = "DISABLE_UPDATES"
	googleApplicationCredentialsEnv = "GOOGLE_APPLICATION_CREDENTIALS"
)

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
	mcpJSON, err := cfg.MCPConfigJSON()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(input.Workspace) || !filepath.IsAbs(input.DurableDir) || !filepath.IsAbs(input.EphemeralDir) {
		return nil, fmt.Errorf("workspace, durable, and ephemeral directories must be absolute paths")
	}
	claudeDir := filepath.Join(input.DurableDir, "claude")
	for _, directory := range []struct{ name, path string }{
		{name: "workspace", path: input.Workspace},
		{name: "Claude state", path: claudeDir},
		{name: "generated Claude skills", path: filepath.Join(claudeDir, "skills")},
	} {
		if err := ensurePrivateDir(directory.path); err != nil {
			return nil, fmt.Errorf("prepare %s directory: %w", directory.name, err)
		}
	}
	if cfg.SkillResources != nil {
		if err := agentplugins.MaterializeSkills(ctx, *cfg.SkillResources, agentplugins.SkillPaths{
			Plugins: filepath.Join(claudeDir, "packages"),
			Skills:  filepath.Join(claudeDir, "skills"),
		}); err != nil {
			return nil, fmt.Errorf("materialize Claude skills: %w", err)
		}
	}
	environment := setEnvironment(input.Environment, claudeConfigDirEnv, claudeDir)
	// The image and compiler pin an exact Claude version. Prevent both automatic
	// and manual update paths from changing that runtime after validation.
	environment = setEnvironment(environment, disableUpdatesEnv, "1")
	environment, err = materializeGoogleCredentials(environment, input.EphemeralDir)
	if err != nil {
		return nil, err
	}
	var mcpConfigPath string
	if len(mcpJSON) != 0 {
		if err := ensurePrivateDir(input.EphemeralDir); err != nil {
			return nil, fmt.Errorf("prepare ephemeral MCP directory: %w", err)
		}
		mcpConfigPath = filepath.Join(input.EphemeralDir, "mcp.json")
		if err := replacePrivateFile(mcpConfigPath, mcpJSON); err != nil {
			return nil, fmt.Errorf("materialize Claude MCP configuration: %w", err)
		}
	}
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: cfg.ClaudeExecutable, ExpectedVersion: cfg.ExpectedClaudeVersion,
		StrictVersion: cfg.StrictVersion, Workspace: input.Workspace, Model: cfg.Model,
		AppendSystemPrompt: cfg.AppendSystemPrompt, AgentsJSON: agentsJSON, MCPConfigPath: mcpConfigPath, Environment: environment,
		MaxEventBytes: cfg.MaxEventBytes, MaxStderrBytes: cfg.MaxStderrBytes,
		InterruptGrace: cfg.InterruptGrace(),
	}), nil
}

func replacePrivateFile(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
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
	if err := ensurePrivateDir(directory); err != nil {
		return nil, fmt.Errorf("prepare ephemeral credentials directory: %w", err)
	}
	path := filepath.Join(directory, "google-credentials.json")
	temporary, err := os.CreateTemp(directory, ".google-credentials-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary Google credentials: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return nil, fmt.Errorf("secure temporary Google credentials: %w", err)
	}
	if _, err := temporary.WriteString(credentials); err != nil {
		_ = temporary.Close()
		return nil, fmt.Errorf("materialize Google credentials: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close Google credentials: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return nil, fmt.Errorf("replace Google credentials: %w", err)
	}
	return setEnvironment(filtered, googleApplicationCredentialsEnv, path), nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%q is not a directory", path)
	}
	return os.Chmod(path, 0o700)
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
