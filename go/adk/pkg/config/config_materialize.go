package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/bootstrap"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

const (
	envKagentToken  = "KAGENT_TOKEN"
	kagentTokenDir  = "/var/run/secrets/tokens"
	kagentTokenFile = "kagent-token"
)

// MaterializeFromBootstrap bootstraps the configuration and writes the working files
// expected by the Go ADK runtime. Legacy payload environment inputs are not read.
func MaterializeFromBootstrap(ctx context.Context, configDir string) error {
	bundle, err := bootstrap.FromEnvironment(ctx, os.Getenv) //nolint:forbidigo // Controller-generated descriptor and SDK trust.
	if err != nil {
		return err
	}
	if err := materializeBootstrapFiles(configDir, bundle); err != nil {
		return err
	}
	if err := materializeEnvToFile(envKagentToken, filepath.Join(kagentTokenDir, kagentTokenFile)); err != nil {
		return fmt.Errorf("materialize kagent token: %w", err)
	}
	return nil
}

func materializeEnvToFile(envKey, path string) error {
	value := strings.TrimSpace(os.Getenv(envKey)) //nolint:forbidigo // Controller-generated payloads and credentials, not user configuration.
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
				resolved, ok := os.LookupEnv(name) //nolint:forbidigo // Resolve arbitrary credential placeholders in compiled configuration.
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

func writeConfigFile(path string, value []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func materializeBootstrapFiles(configDir string, bundle *runtimev1.BootstrapServiceGetBootstrapConfigResponse) error {
	var err error
	value := string(bundle.ConfigJson)
	if strings.Contains(value, "__KAGENT_ENV[") {
		value, err = expandConfigEnv(value)
		if err != nil {
			return err
		}
	}
	if err := writeConfigFile(filepath.Join(configDir, "config.json"), []byte(value)); err != nil {
		return err
	}
	if err := writeConfigFile(filepath.Join(configDir, "agent-card.json"), bundle.AgentCardJson); err != nil {
		return err
	}
	return nil
}
