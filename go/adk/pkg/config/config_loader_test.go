package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func createTempConfigFile(t *testing.T, content string) string {
	tmpfile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	return tmpfile.Name()
}

func TestLoadAgentConfig(t *testing.T) {
	configJSON := `{
  "agent": {
    "model": {
      "model": "gpt-4",
      "openai": {}
    },
    "instruction": "You are a helpful assistant"
  }
}`

	configPath := createTempConfigFile(t, configJSON)
	defer os.Remove(configPath)

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}

	if config == nil {
		t.Fatal("LoadAgentConfig() returned nil config")
		return
	}

	// Check that model was loaded
	if config.Agent.Model == nil {
		t.Error("Expected model to be loaded")
	}

	// Check instruction
	if config.Agent.Instruction != "You are a helpful assistant" {
		t.Errorf("Expected instruction = %q, got %q", "You are a helpful assistant", config.Agent.Instruction)
	}
}

func TestLoadAgentConfig_InvalidJSON(t *testing.T) {
	configPath := createTempConfigFile(t, "invalid json")
	defer os.Remove(configPath)

	_, err := LoadAgentConfig(configPath)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestLoadAgentConfig_FileNotFound(t *testing.T) {
	_, err := LoadAgentConfig("/nonexistent/config.json")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestMaterializeFromEnv(t *testing.T) {
	tmpDir := t.TempDir()

	t.Setenv("KAGENT_CONFIG_JSON", `{
  "agent": {
    "model": {
      "model": "gpt-4",
      "openai": {}
    },
    "instruction": "hi"
  }
}`)
	t.Setenv("KAGENT_AGENT_CARD_JSON", `{
  "name": "test-agent",
  "version": "1.0.0"
}`)

	if err := MaterializeFromEnv(tmpDir); err != nil {
		t.Fatalf("MaterializeFromEnv() error = %v", err)
	}

	config, err := LoadAgentConfig(filepath.Join(tmpDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}
	if config.Agent.Instruction != "hi" {
		t.Fatalf("instruction = %q, want hi", config.Agent.Instruction)
	}

	card, err := LoadAgentCard(filepath.Join(tmpDir, "agent-card.json"))
	if err != nil {
		t.Fatalf("LoadAgentCard() error = %v", err)
	}
	if card.Name != "test-agent" {
		t.Fatalf("card name = %q, want test-agent", card.Name)
	}

	if runtime.GOOS != "windows" {
		for _, name := range []string{"config.json", "agent-card.json"} {
			fi, err := os.Stat(filepath.Join(tmpDir, name))
			if err != nil {
				t.Fatalf("stat %s: %v", name, err)
			}
			if perm := fi.Mode().Perm(); perm != 0o600 {
				t.Errorf("%s permissions = %o, want 0600", name, perm)
			}
		}
	}
}

func TestMaterializeFromEnv_SkipsUnset(t *testing.T) {
	tmpDir := t.TempDir()
	if err := MaterializeFromEnv(tmpDir); err != nil {
		t.Fatalf("MaterializeFromEnv() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no config.json when env unset, stat err = %v", err)
	}
}

func TestLoadAgentCard(t *testing.T) {
	cardJSON := `{
  "name": "test-agent",
  "version": "1.0.0",
  "description": "Test agent"
}`

	cardPath := createTempConfigFile(t, cardJSON)
	defer os.Remove(cardPath)

	card, err := LoadAgentCard(cardPath)
	if err != nil {
		t.Fatalf("LoadAgentCard() error = %v", err)
	}

	if card == nil {
		t.Fatal("LoadAgentCard() returned nil card")
		return
	}

	if card.Name != "test-agent" {
		t.Errorf("Expected name = %q, got %q", "test-agent", card.Name)
	}
}

func TestLoadAgentCard_InvalidJSON(t *testing.T) {
	cardPath := createTempConfigFile(t, "invalid json")
	defer os.Remove(cardPath)

	_, err := LoadAgentCard(cardPath)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestLoadAgentConfigs(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create config.json
	configJSON := `{
  "agent": {
    "model": {
      "model": "gpt-4",
      "openai": {}
    },
    "instruction": "You are a helpful assistant"
  }
}`
	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("Failed to write config.json: %v", err)
	}

	// Create agent-card.json
	cardJSON := `{
  "name": "test-agent",
  "version": "1.0.0"
}`
	cardPath := filepath.Join(tmpDir, "agent-card.json")
	if err := os.WriteFile(cardPath, []byte(cardJSON), 0644); err != nil {
		t.Fatalf("Failed to write agent-card.json: %v", err)
	}

	config, card, err := LoadAgentConfigs(tmpDir)
	if err != nil {
		t.Fatalf("LoadAgentConfigs() error = %v", err)
	}

	if config == nil {
		t.Error("Expected config to be loaded")
		return
	}

	if card == nil {
		t.Error("Expected card to be loaded")
		return
	}

	if config.Agent.Instruction != "You are a helpful assistant" {
		t.Errorf("Expected instruction = %q, got %q", "You are a helpful assistant", config.Agent.Instruction)
	}

	if card.Name != "test-agent" {
		t.Errorf("Expected card name = %q, got %q", "test-agent", card.Name)
	}
}

func TestLoadAgentConfigs_MissingConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, _, err = LoadAgentConfigs(tmpDir)
	if err == nil {
		t.Error("Expected error for missing config.json, got nil")
	}
}

func TestAgentConfig_ModelTypes(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		modelType string
	}{
		{
			name: "OpenAI model",
			config: `{
  "agent": {
    "model": {
      "openai": {},
      "model": "gpt-4"
    }
  }
}`,
			modelType: "openai",
		},
		{
			name: "Anthropic model",
			config: `{
  "agent": {
    "model": {
      "model": "claude-3-opus",
      "anthropic": {}
    }
  }
}`,
			modelType: "anthropic",
		},
		{
			name: "Gemini model",
			config: `{
  "agent": {
    "model": {
      "model": "gemini-pro",
      "gemini": {}
    }
  }
}`,
			modelType: "gemini",
		},
		{
			name: "Ollama model",
			config: `{
  "agent": {
    "model": {
      "model": "llama2",
      "ollama": {}
    }
  }
}`,
			modelType: "ollama",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTempConfigFile(t, tt.config)
			defer os.Remove(configPath)

			config, err := LoadAgentConfig(configPath)
			if err != nil {
				t.Fatalf("LoadAgentConfig() error = %v", err)
			}

			if config.Agent.Model == nil {
				t.Fatal("Expected model to be loaded")
			}

			// Check the selected provider.
			modelType := ModelType(config.Agent.Model)

			if modelType != tt.modelType {
				t.Errorf("Expected model type = %q, got %v", tt.modelType, modelType)
			}
		})
	}
}

func TestAgentConfig_Stream(t *testing.T) {
	configJSON := `{
  "agent": {
    "model": {
      "model": "gpt-4",
      "openai": {}
    }
  }
}`

	configPath := createTempConfigFile(t, configJSON)
	defer os.Remove(configPath)

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}

	// Default stream should be false
	if config.Agent.GetStream() != false {
		t.Errorf("Expected default stream = false, got %v", config.Agent.GetStream())
	}
}

func TestAgentConfig_CustomStream(t *testing.T) {
	configJSON := `{
  "agent": {
    "model": {
      "model": "gpt-4",
      "openai": {}
    },
    "stream": true
  }
}`

	configPath := createTempConfigFile(t, configJSON)
	defer os.Remove(configPath)

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}

	if config.Agent.GetStream() != true {
		t.Errorf("Expected stream = true, got %v", config.Agent.GetStream())
	}
}
