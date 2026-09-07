// Package adapter validates and materializes compiler-owned Codex state.
package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/harness/codex/internal/driver"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/pelletier/go-toml/v2"
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
	for _, directory := range []string{input.Workspace, codexHome, filepath.Join(codexHome, "agents"), filepath.Join(codexHome, "skills")} {
		if err := utils.EnsurePrivateDir(directory); err != nil {
			return nil, fmt.Errorf("prepare Codex directory %q: %w", directory, err)
		}
	}
	if err := reconcileGeneratedDir(filepath.Join(codexHome, "agents"), agentFileNames(cfg.Agents)); err != nil {
		return nil, fmt.Errorf("reconcile Codex agents: %w", err)
	}
	if err := reconcileGeneratedDir(filepath.Join(codexHome, "skills"), nil); err != nil {
		return nil, fmt.Errorf("reconcile Codex skills: %w", err)
	}
	if cfg.SkillResources != nil {
		if _, err := agentplugins.Materialize(ctx, *cfg.SkillResources, agentplugins.Paths{
			Packages: filepath.Join(codexHome, "packages"),
			Skills:   filepath.Join(codexHome, "skills"),
		}); err != nil {
			return nil, fmt.Errorf("materialize Codex skills: %w", err)
		}
	}
	if err := materializeAgents(codexHome, cfg.Agents); err != nil {
		return nil, err
	}
	configTOML, err := renderConfig(cfg, codexHome)
	if err != nil {
		return nil, err
	}
	if err := utils.ReplacePrivateFile(filepath.Join(codexHome, "config.toml"), configTOML); err != nil {
		return nil, fmt.Errorf("materialize Codex configuration: %w", err)
	}
	environment := setEnvironment(input.Environment, codexHomeEnv, codexHome)
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: cfg.CodexExecutable, ExpectedVersion: cfg.ExpectedCodexVersion, StrictVersion: cfg.StrictVersion,
		Workspace: input.Workspace, Model: cfg.Model, Provider: nativeProviderName(cfg.Provider.Name),
		DeveloperInstruction: cfg.DeveloperInstruction, Environment: environment,
		MaxFrameBytes: cfg.MaxFrameBytes, MaxStderrBytes: cfg.MaxStderrBytes, InterruptGrace: cfg.InterruptGrace(),
	}), nil
}

type nativeConfig struct {
	Model          string                         `toml:"model"`
	ModelProvider  string                         `toml:"model_provider"`
	ApprovalPolicy string                         `toml:"approval_policy"`
	SandboxMode    string                         `toml:"sandbox_mode"`
	WebSearch      string                         `toml:"web_search"`
	Analytics      nativeAnalytics                `toml:"analytics"`
	ModelProviders map[string]nativeModelProvider `toml:"model_providers,omitempty"`
	Agents         map[string]nativeAgent         `toml:"agents,omitempty"`
	MCPServers     map[string]nativeMCPServer     `toml:"mcp_servers,omitempty"`
}

type nativeAnalytics struct {
	Enabled bool `toml:"enabled"`
}

type nativeModelProvider struct {
	Name    string `toml:"name"`
	WireAPI string `toml:"wire_api"`
	EnvKey  string `toml:"env_key"`
	BaseURL string `toml:"base_url,omitempty"`
}

type nativeAgent struct {
	Description string `toml:"description"`
	ConfigFile  string `toml:"config_file"`
}

type nativeMCPServer struct {
	URL            string            `toml:"url"`
	HTTPHeaders    map[string]string `toml:"http_headers,omitempty,inline"`
	EnvHTTPHeaders map[string]string `toml:"env_http_headers,omitempty,inline"`
	EnabledTools   []string          `toml:"enabled_tools,omitempty"`
}

type nativeAgentConfig struct {
	Model                 string `toml:"model"`
	DeveloperInstructions string `toml:"developer_instructions"`
}

// renderConfig translates the versioned Kagent contract into the pinned
// Codex CLI's native TOML without copying credential values into durable state.
func renderConfig(cfg config.Config, codexHome string) ([]byte, error) {
	native := nativeConfig{
		Model: cfg.Model, ModelProvider: nativeProviderName(cfg.Provider.Name),
		ApprovalPolicy: "never", SandboxMode: "danger-full-access", WebSearch: "disabled",
		Analytics:  nativeAnalytics{Enabled: false},
		Agents:     make(map[string]nativeAgent, len(cfg.Agents)),
		MCPServers: make(map[string]nativeMCPServer, len(cfg.MCPServers)),
	}
	if cfg.Provider.Name == "openai" {
		native.ModelProviders = map[string]nativeModelProvider{
			"kagent-openai": {
				Name: "OpenAI", WireAPI: "responses", EnvKey: "OPENAI_API_KEY", BaseURL: cfg.Provider.BaseURL,
			},
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
		native.MCPServers[name] = nativeMCPServer{
			URL: server.URL, HTTPHeaders: literal, EnvHTTPHeaders: environment, EnabledTools: server.EnabledTools,
		}
	}
	contents, err := toml.Marshal(native)
	if err != nil {
		return nil, fmt.Errorf("encode Codex configuration: %w", err)
	}
	return contents, nil
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
