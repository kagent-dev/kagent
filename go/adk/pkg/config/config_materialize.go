package config

import (
	"encoding/json"
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

// MaterializeFromEnv writes Agent Substrate environment variables to
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
	if envKey == envAgentConfigJSON && strings.Contains(value, "__KAGENT_ENV[") {
		expanded, err := expandConfigEnv(value)
		if err != nil {
			return err
		}
		value = expanded
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

func expandConfigEnv(raw string) (string, error) {
	var config any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return "", fmt.Errorf("decode config JSON: %w", err)
	}
	var expand func(any) (any, error)
	expand = func(value any) (any, error) {
		switch value := value.(type) {
		case string:
			const prefix, suffix = "__KAGENT_ENV[", "]__"
			if strings.HasPrefix(value, prefix) && strings.HasSuffix(value, suffix) {
				name := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
				resolved, ok := os.LookupEnv(name)
				if !ok {
					return nil, fmt.Errorf("required environment variable %s is not set", name)
				}
				return resolved, nil
			}
		case []any:
			for i := range value {
				item, err := expand(value[i])
				if err != nil {
					return nil, err
				}
				value[i] = item
			}
		case map[string]any:
			for key, item := range value {
				expanded, err := expand(item)
				if err != nil {
					return nil, err
				}
				value[key] = expanded
			}
		}
		return value, nil
	}
	expanded, err := expand(config)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(expanded)
	if err != nil {
		return "", fmt.Errorf("encode config JSON: %w", err)
	}
	return string(encoded), nil
}
