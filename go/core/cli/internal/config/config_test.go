package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func TestConfigFileValuesAreUsed(t *testing.T) {
	// Reset viper for a clean test
	viper.Reset()

	// Create a temp config directory
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write a config file with custom values
	configContent := `kagent_url: http://custom-server:9090
namespace: my-namespace
output_format: json
verbose: true
timeout: 60s
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set up viper to read the config
	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")
	viper.SetDefault("kagent_url", "http://localhost:8083")
	viper.SetDefault("output_format", "table")
	viper.SetDefault("namespace", "kagent")
	viper.SetDefault("timeout", 300*time.Second)

	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	// Create flags (simulating what main.go does)
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("kagent-url", "http://localhost:8083", "KAgent URL")
	flags.StringP("namespace", "n", "kagent", "Namespace")
	flags.StringP("output-format", "o", "table", "Output format")
	flags.BoolP("verbose", "v", false, "Verbose output")
	flags.Duration("timeout", 300*time.Second, "Timeout")

	// Bind flags to viper
	BindFlags(flags)

	// Get merged config (should use config file values since no flags were explicitly set)
	cfg := &Config{}
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if cfg.KAgentURL != "http://custom-server:9090" {
		t.Errorf("Expected KAgentURL=http://custom-server:9090, got %s", cfg.KAgentURL)
	}
	if cfg.Namespace != "my-namespace" {
		t.Errorf("Expected Namespace=my-namespace, got %s", cfg.Namespace)
	}
	if cfg.OutputFormat != "json" {
		t.Errorf("Expected OutputFormat=json, got %s", cfg.OutputFormat)
	}
	if !cfg.Verbose {
		t.Error("Expected Verbose=true, got false")
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("Expected Timeout=60s, got %v", cfg.Timeout)
	}
}

func TestCLIFlagsOverrideConfigFile(t *testing.T) {
	// Reset viper for a clean test
	viper.Reset()

	// Create a temp config directory
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write a config file with custom values
	configContent := `kagent_url: http://config-server:9090
namespace: config-ns
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")
	viper.SetDefault("kagent_url", "http://localhost:8083")
	viper.SetDefault("namespace", "kagent")
	viper.SetDefault("output_format", "table")
	viper.SetDefault("timeout", 300*time.Second)

	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	// Create and parse flags with explicit values
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("kagent-url", "http://localhost:8083", "KAgent URL")
	flags.StringP("namespace", "n", "kagent", "Namespace")
	flags.StringP("output-format", "o", "table", "Output format")
	flags.BoolP("verbose", "v", false, "Verbose output")
	flags.Duration("timeout", 300*time.Second, "Timeout")

	// Simulate passing --kagent-url on the CLI
	if err := flags.Parse([]string{"--kagent-url", "http://cli-override:7777"}); err != nil {
		t.Fatalf("Failed to parse flags: %v", err)
	}

	BindFlags(flags)

	cfg := &Config{}
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// CLI flag should win over config file
	if cfg.KAgentURL != "http://cli-override:7777" {
		t.Errorf("Expected KAgentURL=http://cli-override:7777, got %s", cfg.KAgentURL)
	}
	// Config file value should still be used for namespace (not overridden by CLI)
	if cfg.Namespace != "config-ns" {
		t.Errorf("Expected Namespace=config-ns, got %s", cfg.Namespace)
	}
}
