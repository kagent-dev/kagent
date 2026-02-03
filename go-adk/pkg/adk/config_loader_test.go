package adk

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		"model": {
			"type": "openai",
			"name": "gpt-4",
			"api_key": "test-key"
		},
		"instruction": "You are a helpful assistant",
		"timeout": 1800.0
	}`

	configPath := createTempConfigFile(t, configJSON)
	defer os.Remove(configPath)

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}

	if config == nil {
		t.Fatal("LoadAgentConfig() returned nil config")
	}

	// Check that model was loaded
	if config.Model == nil {
		t.Error("Expected model to be loaded")
	}

	// Check instruction
	if config.Instruction != "You are a helpful assistant" {
		t.Errorf("Expected instruction = %q, got %q", "You are a helpful assistant", config.Instruction)
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
		"model": {
			"type": "openai",
			"name": "gpt-4",
			"api_key": "test-key"
		},
		"instruction": "You are a helpful assistant"
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

	if config.Instruction != "You are a helpful assistant" {
		t.Errorf("Expected instruction = %q, got %q", "You are a helpful assistant", config.Instruction)
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
				"model": {
					"type": "openai",
					"name": "gpt-4",
					"api_key": "test-key"
				}
			}`,
			modelType: "openai",
		},
		{
			name: "Anthropic model",
			config: `{
				"model": {
					"type": "anthropic",
					"name": "claude-3-opus",
					"api_key": "test-key"
				}
			}`,
			modelType: "anthropic",
		},
		{
			name: "Gemini model",
			config: `{
				"model": {
					"type": "gemini",
					"name": "gemini-pro",
					"api_key": "test-key"
				}
			}`,
			modelType: "gemini",
		},
		{
			name: "Ollama model",
			config: `{
				"model": {
					"type": "ollama",
					"name": "llama2",
					"base_url": "http://localhost:11434"
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

			if config.Model == nil {
				t.Fatal("Expected model to be loaded")
			}

			// Check model type by unmarshaling to check the type field
			var modelMap map[string]interface{}
			modelJSON, _ := json.Marshal(config.Model)
			if err := json.Unmarshal(modelJSON, &modelMap); err != nil {
				t.Fatalf("unmarshal model: %v", err)
			}

			if modelType, ok := modelMap["type"].(string); !ok || modelType != tt.modelType {
				t.Errorf("Expected model type = %q, got %v", tt.modelType, modelMap["type"])
			}
		})
	}
}

func TestAgentConfig_Stream(t *testing.T) {
	configJSON := `{
		"model": {
			"type": "openai",
			"name": "gpt-4",
			"api_key": "test-key"
		}
	}`

	configPath := createTempConfigFile(t, configJSON)
	defer os.Remove(configPath)

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}

	// Default stream should be false
	if config.GetStream() != false {
		t.Errorf("Expected default stream = false, got %v", config.GetStream())
	}
}

func TestAgentConfig_CustomStream(t *testing.T) {
	configJSON := `{
		"model": {
			"type": "openai",
			"name": "gpt-4",
			"api_key": "test-key"
		},
		"stream": true
	}`

	configPath := createTempConfigFile(t, configJSON)
	defer os.Remove(configPath)

	config, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}

	if config.GetStream() != true {
		t.Errorf("Expected stream = true, got %v", config.GetStream())
	}
}
