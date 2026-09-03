// Package config defines the versioned, non-secret Codex Harness runtime
// configuration shared by its compiler and Actor entrypoint.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
)

const (
	Version            = 1
	PinnedCodexVersion = "0.148.0"
)

var nativeNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Config is compiler-owned input to the native adapter. Credential values are
// supplied only through the process environment.
type Config struct {
	Version              int                    `json:"version"`
	CodexExecutable      string                 `json:"codex_executable"`
	ExpectedCodexVersion string                 `json:"expected_codex_version"`
	StrictVersion        bool                   `json:"strict_version"`
	Model                string                 `json:"model"`
	Provider             Provider               `json:"provider"`
	DeveloperInstruction string                 `json:"developer_instruction,omitempty"`
	Agents               map[string]Agent       `json:"agents,omitempty"`
	SkillResources       *agentplugin.Resources `json:"skill_resources,omitempty"`
	MCPServers           map[string]MCPServer   `json:"mcp_servers,omitempty"`
	MaxFrameBytes        int                    `json:"max_frame_bytes"`
	MaxStderrBytes       int                    `json:"max_stderr_bytes"`
	InterruptGraceMillis int                    `json:"interrupt_grace_millis"`
}

type Provider struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url,omitempty"`
}

type Agent struct {
	Description string `json:"description"`
	Instruction string `json:"instruction"`
	Model       string `json:"model"`
}

type MCPServer struct {
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	EnabledTools    []string          `json:"enabled_tools,omitempty"`
	RequireApproval bool              `json:"require_approval,omitempty"`
}

func Production(model, instruction string) Config {
	return Config{
		Version: Version, CodexExecutable: "codex", ExpectedCodexVersion: PinnedCodexVersion,
		StrictVersion: true, Model: model, DeveloperInstruction: instruction,
		MaxFrameBytes: 1 << 20, MaxStderrBytes: 64 << 10, InterruptGraceMillis: 2000,
	}
}

func Parse(data []byte) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
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
	if strings.TrimSpace(c.CodexExecutable) == "" || strings.TrimSpace(c.Model) == "" || strings.TrimSpace(c.Provider.Name) == "" {
		return fmt.Errorf("codex executable, model, and provider are required")
	}
	if c.StrictVersion && strings.TrimSpace(c.ExpectedCodexVersion) == "" {
		return fmt.Errorf("expected_codex_version is required when strict_version is enabled")
	}
	if c.MaxFrameBytes <= 0 || c.MaxStderrBytes <= 0 || c.InterruptGraceMillis <= 0 {
		return fmt.Errorf("frame, stderr, and interrupt grace limits must be positive")
	}
	if c.Provider.Name != "openai" && c.Provider.Name != "amazon-bedrock" {
		return fmt.Errorf("unsupported Codex provider %q", c.Provider.Name)
	}
	if c.Provider.BaseURL != "" {
		if c.Provider.Name != "openai" {
			return fmt.Errorf("base URL is supported only for the OpenAI provider")
		}
		if err := validateURL(c.Provider.BaseURL); err != nil {
			return fmt.Errorf("invalid provider base URL: %w", err)
		}
	}
	for name, agent := range c.Agents {
		if !nativeNamePattern.MatchString(name) {
			return fmt.Errorf("codex agent name %q must contain only letters, numbers, underscores, or hyphens", name)
		}
		if strings.TrimSpace(agent.Description) == "" || strings.TrimSpace(agent.Instruction) == "" || strings.TrimSpace(agent.Model) == "" {
			return fmt.Errorf("codex agent %q requires description, instruction, and model", name)
		}
	}
	for name, server := range c.MCPServers {
		if !nativeNamePattern.MatchString(name) {
			return fmt.Errorf("codex MCP server name %q must contain only letters, numbers, underscores, or hyphens", name)
		}
		if err := validateURL(server.URL); err != nil {
			return fmt.Errorf("invalid Codex MCP server %q URL: %w", name, err)
		}
		seen := map[string]struct{}{}
		for _, tool := range server.EnabledTools {
			if strings.TrimSpace(tool) == "" {
				return fmt.Errorf("codex MCP server %q has an empty enabled tool", name)
			}
			if _, ok := seen[tool]; ok {
				return fmt.Errorf("codex MCP server %q has duplicate enabled tool %q", name, tool)
			}
			seen[tool] = struct{}{}
		}
	}
	return nil
}

func validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return nil
}

func (c Config) InterruptGrace() time.Duration {
	return time.Duration(c.InterruptGraceMillis) * time.Millisecond
}
