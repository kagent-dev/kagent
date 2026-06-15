package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	envAgentConfigJSON = "KAGENT_CONFIG_JSON"
	envAgentCardJSON   = "KAGENT_AGENT_CARD_JSON"
	envSRTSettingsJSON = "KAGENT_SRT_SETTINGS_JSON"
	envKagentToken     = "KAGENT_TOKEN"
	kagentTokenDir     = "/var/run/secrets/tokens"
	kagentTokenFile    = "kagent-token"
	srtSettingsFile    = "srt-settings.json"
)

// MaterializeFromEnv writes Agent Substrate secret-backed environment variables to
// the on-disk paths expected by the Go ADK runtime at startup.
func MaterializeFromEnv(configDir string) error {
	if err := materializeEnvToFile(envAgentConfigJSON, filepath.Join(configDir, "config.json")); err != nil {
		return fmt.Errorf("materialize agent config: %w", err)
	}
	if err := materializeEnvToFile(envAgentCardJSON, filepath.Join(configDir, "agent-card.json")); err != nil {
		return fmt.Errorf("materialize agent card: %w", err)
	}
	if err := materializeEnvToFile(envSRTSettingsJSON, filepath.Join(configDir, srtSettingsFile)); err != nil {
		return fmt.Errorf("materialize srt settings: %w", err)
	}
	if err := materializeEnvToFile(envKagentToken, filepath.Join(kagentTokenDir, kagentTokenFile)); err != nil {
		return fmt.Errorf("materialize kagent token: %w", err)
	}
	return nil
}

func materializeEnvToFile(envKey, path string) error {
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}
