// Package config loads explicit CLI and controller configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"sigs.k8s.io/yaml"
)

// Load decodes a YAML or JSON config file. Defaults and validation run after
// explicit command-line overrides have been applied.
func Load(path string, dst proto.Message) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config %s: %w", path, err)
	}
	if err := Decode(data, dst); err != nil {
		return fmt.Errorf("failed to decode config %s: %w", path, err)
	}
	return nil
}

// Decode uses strict YAML conversion followed by ProtoJSON decoding. YAML aliases
// and scalar resolution follow sigs.k8s.io/yaml; only the first document is read.
func Decode(data []byte, dst proto.Message) error {
	encoded, err := yaml.YAMLToJSONStrict(data)
	if err != nil {
		return fmt.Errorf("failed to convert configuration to JSON: %w", err)
	}
	if err := protojson.Unmarshal(encoded, dst); err != nil {
		return fmt.Errorf("invalid configuration fields: %w", err)
	}
	return nil
}

// Validate evaluates the source .proto rules without accessing external state.
func Validate(cfg proto.Message) error {
	if err := protovalidate.Validate(cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	return nil
}

// FilePath resolves paths in a document relative to that document.
func FilePath(configPath, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(filepath.Dir(configPath), value)
}

// ReadSecret reads a referenced secret without including its contents in errors.
func ReadSecret(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("secret file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file %s: %w", path, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("secret file %s is empty", path)
	}
	return value, nil
}
