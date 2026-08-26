// Package config defines the versioned, non-secret Claude Harness runtime
// configuration shared by its compiler and Actor entrypoint.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var agentNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const (
	Version                      = 2
	PinnedClaudeVersion          = "2.1.217"
	GoogleCredentialsJSONEnvName = "KAGENT_CLAUDE_GOOGLE_CREDENTIALS_JSON"
)

type Config struct {
	Version               int              `json:"version"`
	ClaudeExecutable      string           `json:"claude_executable"`
	ExpectedClaudeVersion string           `json:"expected_claude_version"`
	StrictVersion         bool             `json:"strict_version"`
	Model                 string           `json:"model,omitempty"`
	AppendSystemPrompt    string           `json:"append_system_prompt,omitempty"`
	Agents                map[string]Agent `json:"agents,omitempty"`
	MaxEventBytes         int              `json:"max_event_bytes"`
	MaxStderrBytes        int              `json:"max_stderr_bytes"`
	InterruptGraceMillis  int              `json:"interrupt_grace_millis"`
}

// Agent is one compiler-owned local Claude subagent passed through --agents.
// Permission mode, hooks, memory, isolation, and inline MCP are deliberately
// omitted from the supported contract.
type Agent struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model,omitempty"`
}

func Production(model, instruction string) Config {
	return Config{
		Version: Version, ClaudeExecutable: "claude",
		ExpectedClaudeVersion: PinnedClaudeVersion, StrictVersion: true,
		Model: model, AppendSystemPrompt: instruction,
		MaxEventBytes: 1 << 20, MaxStderrBytes: 64 << 10,
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
	for name, agent := range c.Agents {
		if !agentNamePattern.MatchString(name) {
			return fmt.Errorf("Claude agent name %q must contain only letters, numbers, underscores, or hyphens", name)
		}
		if strings.TrimSpace(agent.Description) == "" || strings.TrimSpace(agent.Prompt) == "" {
			return fmt.Errorf("Claude agent %q requires a non-empty description and prompt", name)
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

func (c Config) InterruptGrace() time.Duration {
	return time.Duration(c.InterruptGraceMillis) * time.Millisecond
}
