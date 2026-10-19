// Package adapter materializes the omp state an Agent's configuration asks for
// and pins the flags that keep a workspace from extending it.
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
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/omp/internal/config"
	"github.com/kagent-dev/kagent/go/harness/omp/internal/driver"
	"github.com/kagent-dev/kagent/go/harness/workspace"
)

const (
	// providerName is the omp provider the Agent's model is declared under.
	providerName = "kagent"
	// apiKeyEnv holds the controller's placeholder for an Anthropic
	// ModelConfig's Secret; the egress gateway replaces it on the model host.
	apiKeyEnv = "ANTHROPIC_API_KEY"
	// apiKeyHeader is the header the egress gateway sets on the model host.
	apiKeyHeader = "X-Api-Key"

	maxFrameBytes  = 1 << 20
	maxStderrBytes = 64 << 10
	interruptGrace = 10 * time.Second

	// configEnv is the compiled configuration, which names every server and
	// header; omp is given what it needs as files instead.
	configEnv = "KAGENT_CONFIG_JSON"
)

// disabledSources are omp's discovery sources besides its own native one,
// and the local model providers it would otherwise probe. A repository's
// .claude, .codex, .cursor or other configuration reaches omp only through
// them, so none adds a tool, server, rule or provider.
var disabledSources = []string{
	"agent-plugins", "agents", "agents-md", "claude", "claude-md", "claude-plugins", "cline", "codex",
	"cursor", "custom-context", "gemini", "github", "mcp-json", "omp-plugins", "opencode", "skillshare",
	"ssh-json", "vscode", "windsurf",
	"ollama", "llama.cpp", "lm-studio",
}

// Input contains the compiled configuration and the Actor-owned locations.
type Input struct {
	ConfigJSON []byte
	Executable string
	Workspace  string
	DurableDir string
	// Environment is the process environment omp inherits; LookupEnv reads
	// the same variables while the configuration is parsed.
	Environment []string
}

// New validates the configuration, writes omp's files and returns the driver.
func New(ctx context.Context, input Input) (*driver.ProcessDriver, error) {
	lookupEnv := func(name string) (string, bool) { return lookup(input.Environment, name) }
	cfg, err := config.Parse(input.ConfigJSON, lookupEnv)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(input.Workspace) || !filepath.IsAbs(input.DurableDir) {
		return nil, fmt.Errorf("workspace and durable directories must be absolute paths")
	}
	paths := newPaths(input.DurableDir)
	for _, directory := range []string{input.Workspace, paths.home, paths.agent, paths.skills, paths.sessions} {
		if err := utils.EnsurePrivateDir(directory); err != nil {
			return nil, fmt.Errorf("prepare omp directory %q: %w", directory, err)
		}
	}
	if err := materializeSkills(ctx, cfg, paths); err != nil {
		return nil, err
	}
	actorJWT := &turnActorJWTHosts{}
	bridged, err := startBridges(ctx, cfg.MCPServers, actorJWT)
	if err != nil {
		return nil, err
	}
	_, hasKey := lookupEnv(apiKeyEnv)
	prepare := func(actorJWTHosts []string) error {
		actorJWT.set(actorJWTHosts)
		files := ompFiles{
			paths.overlay: settingsOverlay(),
			paths.models:  modelsFile(cfg.Model, hasKey),
			paths.mcp:     mcpFile(cfg.MCPServers, bridged, actorJWTHosts),
		}
		return files.write(paths, cfg.Instruction)
	}
	if err := prepare(nil); err != nil {
		return nil, err
	}
	return driver.NewProcessDriver(driver.ProcessConfig{
		Executable: input.Executable, ExpectedVersion: config.PinnedOmpVersion, Args: runArgs(cfg, input.Workspace, paths),
		Workspace: input.Workspace, SessionDir: paths.sessions,
		Environment:   nativeEnvironment(input.Environment, paths, heldByBridges(cfg.MCPServers, bridged)),
		MaxFrameBytes: maxFrameBytes, MaxStderrBytes: maxStderrBytes, InterruptGrace: interruptGrace,
		Prepare: prepare,
	}), nil
}

// ompFiles are the configuration files the adapter owns, by path.
type ompFiles map[string]any

// write replaces every file the adapter owns and the instruction.
func (files ompFiles) write(paths paths, instruction string) error {
	for path, contents := range files {
		encoded, err := json.MarshalIndent(contents, "", "  ")
		if err != nil {
			return fmt.Errorf("encode omp file %q: %w", path, err)
		}
		if err := utils.ReplacePrivateFile(path, append(encoded, '\n')); err != nil {
			return fmt.Errorf("materialize omp file %q: %w", path, err)
		}
	}
	if err := utils.ReplacePrivateFile(paths.instruction, []byte(instruction)); err != nil {
		return fmt.Errorf("materialize omp instruction: %w", err)
	}
	return nil
}

// paths are omp's locations under the durable directory: its home (logs,
// native modules, run state), its agent directory (configuration and skills)
// and the session directory a resumed turn reads its conversation from.
type paths struct {
	home, agent, skills, sessions, overlay, models, mcp, instruction string
}

func newPaths(durableDir string) paths {
	root := filepath.Join(durableDir, "omp")
	agent := filepath.Join(root, "agent")
	return paths{
		home: filepath.Join(root, "home"), agent: agent, skills: filepath.Join(agent, "skills"),
		sessions: filepath.Join(root, "sessions"), overlay: filepath.Join(agent, "kagent.yml"),
		models: filepath.Join(agent, "models.yml"), mcp: filepath.Join(agent, "mcp.json"),
		instruction: filepath.Join(agent, "instruction.md"),
	}
}

// runArgs are the flags of every omp run. --no-extensions keeps ambient
// extensions, hooks and plugins out and --no-rules a workspace's rules; the
// overlay covers what has no flag. The instruction is passed as a file so its
// text is never read as a path.
func runArgs(cfg config.Config, workspace string, paths paths) []string {
	args := []string{
		"--cwd", workspace, "--config", paths.overlay, "--model", providerName + "/" + cfg.Model.ID,
		"--no-extensions", "--no-rules", "--no-title", "--approval-mode", "yolo",
	}
	if strings.TrimSpace(cfg.Instruction) != "" {
		args = append(args, "--append-system-prompt", paths.instruction)
	}
	return args
}

type settings struct {
	DisabledProviders []string       `json:"disabledProviders"`
	EnabledProviders  []string       `json:"enabledProviders"`
	MCP               mcpSettings    `json:"mcp"`
	Skills            skillSettings  `json:"skills"`
	Startup           startupSetting `json:"startup"`
	Dev               devSettings    `json:"dev"`
	Tools             toolSettings   `json:"tools"`
}

type mcpSettings struct {
	EnableProjectConfig bool `json:"enableProjectConfig"`
	// StartupTimeoutMs is how long omp waits for its servers' tools before
	// the first prompt. Each turn is a new process, so a server that answers
	// later than omp's default window would be missing from the turn.
	StartupTimeoutMs int `json:"startupTimeoutMs"`
}

type skillSettings struct {
	EnablePiUser        bool `json:"enablePiUser"`
	EnablePiProject     bool `json:"enablePiProject"`
	EnableAgentsUser    bool `json:"enableAgentsUser"`
	EnableAgentsProject bool `json:"enableAgentsProject"`
	EnableClaudeUser    bool `json:"enableClaudeUser"`
	EnableClaudeProject bool `json:"enableClaudeProject"`
	EnableCodexUser     bool `json:"enableCodexUser"`
}

type startupSetting struct {
	CheckUpdate bool `json:"checkUpdate"`
}

type devSettings struct {
	AutoQA bool `json:"autoqa"`
}

type toolSettings struct {
	ApprovalMode string `json:"approvalMode"`
	// Xdev mounts MCP tools as xd:// devices the model drives through write;
	// off, each is a tool of its own, and its calls stream under its name.
	Xdev bool `json:"xdev"`
}

// settingsOverlay is the --config overlay, the highest configuration layer
// after flags: it replaces the arrays and booleans a workspace's
// .omp/config.yml could set. omp reads YAML, of which JSON is a subset.
func settingsOverlay() settings {
	return settings{
		DisabledProviders: disabledSources, EnabledProviders: []string{},
		MCP:     mcpSettings{EnableProjectConfig: false, StartupTimeoutMs: 30000},
		Skills:  skillSettings{EnablePiUser: true},
		Startup: startupSetting{CheckUpdate: false},
		Dev:     devSettings{AutoQA: false},
		Tools:   toolSettings{ApprovalMode: "yolo", Xdev: false},
	}
}

type modelsFileContents struct {
	Providers map[string]provider `json:"providers"`
}

type provider struct {
	BaseURL string            `json:"baseUrl"`
	API     string            `json:"api"`
	Auth    string            `json:"auth"`
	Headers map[string]string `json:"headers,omitempty"`
	Models  []model           `json:"models"`
}

type model struct {
	ID        string `json:"id"`
	MaxTokens int    `json:"maxTokens,omitempty"`
}

// modelsFile declares the Agent's model as omp's only custom provider.
// omp's own key handling sends a custom endpoint's key as a bearer token,
// while the egress gateway replaces the x-api-key header of an Anthropic
// ModelConfig, so the provider has no key of its own (auth none sends no
// Authorization) and the key travels as that header. omp resolves a header
// value naming a variable to the variable's value: the placeholder.
// Omitted model metadata (window, cost) comes from omp's catalog by ID.
func modelsFile(m config.Model, hasKey bool) modelsFileContents {
	headers := maps.Clone(m.Headers)
	if hasKey {
		if headers == nil {
			headers = map[string]string{}
		}
		maps.DeleteFunc(headers, func(name, _ string) bool { return strings.EqualFold(name, apiKeyHeader) })
		headers[apiKeyHeader] = apiKeyEnv
	}
	return modelsFileContents{Providers: map[string]provider{providerName: {
		BaseURL: m.BaseURL, API: "anthropic-messages", Auth: "none", Headers: headers,
		Models: []model{{ID: m.ID, MaxTokens: m.MaxTokens}},
	}}}
}

type mcpFileContents struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout int64             `json:"timeout,omitempty"`
}

// mcpFile lists the Agent's servers in omp's agent-directory mcp.json, the
// only MCP configuration the overlay leaves enabled. A server whose tools are
// selected is reached through its bridge, which holds its headers. A server
// reached directly on one of actorJWTHosts that sets no Authorization of its
// own sends the placeholder the egress gateway replaces with the Actor's JWT.
func mcpFile(servers []config.MCPServer, bridged map[string]string, actorJWTHosts []string) mcpFileContents {
	result := mcpFileContents{MCPServers: make(map[string]mcpServer, len(servers))}
	for _, server := range servers {
		entry := mcpServer{Type: "http", URL: server.URL, Headers: server.Headers, Timeout: int64(server.TimeoutSeconds * 1000)}
		if url, ok := bridged[server.Name]; ok {
			entry.URL, entry.Headers = url, nil
		} else {
			entry.Headers = workspace.ActorJWTHeaders(server.URL, server.Headers, actorJWTHosts)
		}
		result.MCPServers[server.Name] = entry
	}
	return result
}

// materializeSkills writes the Agent's skills to omp's user skills directory,
// removing those a previous revision wrote.
func materializeSkills(ctx context.Context, cfg config.Config, paths paths) error {
	if err := os.RemoveAll(paths.skills); err != nil {
		return fmt.Errorf("reset omp skills: %w", err)
	}
	if err := utils.EnsurePrivateDir(paths.skills); err != nil {
		return fmt.Errorf("prepare omp skills: %w", err)
	}
	if cfg.Skills == nil {
		return nil
	}
	if _, err := agentplugins.Materialize(ctx, *cfg.Skills, agentplugins.Paths{
		Packages: filepath.Join(filepath.Dir(paths.agent), "packages"), Skills: paths.skills,
	}); err != nil {
		return fmt.Errorf("materialize omp skills: %w", err)
	}
	return nil
}

// heldByBridges are the header variables of the bridged servers: their
// bridge sends those headers, so omp and the shell commands it runs never
// read the placeholders and cannot reach a server's unselected tools with them.
func heldByBridges(servers []config.MCPServer, bridged map[string]string) []string {
	var variables []string
	for _, server := range servers {
		if _, ok := bridged[server.Name]; ok {
			variables = append(variables, server.HeaderVariables...)
		}
	}
	return variables
}

// nativeEnvironment points omp's home and agent directory into the durable
// directory (omp writes logs, native modules and run state under its home)
// and leaves out the configuration document and the variables withheld.
func nativeEnvironment(environment []string, paths paths, withheld []string) []string {
	for _, name := range append([]string{configEnv}, withheld...) {
		environment = unsetEnvironment(environment, name)
	}
	environment = setEnvironment(environment, "HOME", paths.home)
	return setEnvironment(environment, "PI_CODING_AGENT_DIR", paths.agent)
}

func setEnvironment(environment []string, name, value string) []string {
	return append(unsetEnvironment(environment, name), name+"="+value)
}

func unsetEnvironment(environment []string, name string) []string {
	prefix := name + "="
	return slices.DeleteFunc(slices.Clone(environment), func(item string) bool { return strings.HasPrefix(item, prefix) })
}

func lookup(environment []string, name string) (string, bool) {
	prefix := name + "="
	for _, item := range slices.Backward(environment) {
		if value, ok := strings.CutPrefix(item, prefix); ok {
			return value, true
		}
	}
	return "", false
}
