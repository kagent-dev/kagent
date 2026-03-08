package cli

import (
	"strings"
	"testing"
)

func TestValidateModelProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{name: "valid OpenAI", provider: "OpenAI", wantErr: false},
		{name: "valid Anthropic", provider: "Anthropic", wantErr: false},
		{name: "valid Gemini", provider: "Gemini", wantErr: false},
		{name: "invalid provider", provider: "InvalidProvider", wantErr: true},
		{name: "empty provider", provider: "", wantErr: true},
		{name: "lowercase openai", provider: "openai", wantErr: true}, // Case-sensitive
		{name: "Azure OpenAI not supported", provider: "AzureOpenAI", wantErr: true},
		{name: "Ollama not supported", provider: "Ollama", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelProvider(tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateModelProvider(%q) error = %v, wantErr %v", tt.provider, err, tt.wantErr)
			}
		})
	}
}

func TestInitCfg_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *InitCfg
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid agent name - empty",
			cfg: &InitCfg{
				Framework: "adk",
				Language:  "python",
				AgentName: "",
			},
			wantErr: true,
			errMsg:  "agent name cannot be empty",
		},
		{
			name: "invalid agent name - starts with digit",
			cfg: &InitCfg{
				Framework: "adk",
				Language:  "python",
				AgentName: "1agent",
			},
			wantErr: true,
			errMsg:  "must start with a letter or underscore",
		},
		{
			name: "invalid framework",
			cfg: &InitCfg{
				Framework: "unsupported",
				Language:  "python",
				AgentName: "test_agent",
			},
			wantErr: true,
			errMsg:  "unsupported framework",
		},
		{
			name: "invalid language",
			cfg: &InitCfg{
				Framework: "adk",
				Language:  "javascript",
				AgentName: "test_agent",
			},
			wantErr: true,
			errMsg:  "unsupported language",
		},
		{
			name: "model name without provider",
			cfg: &InitCfg{
				Framework:     "adk",
				Language:      "python",
				AgentName:     "test_agent",
				ModelName:     "gpt-4",
				ModelProvider: "",
			},
			wantErr: true,
			errMsg:  "model provider is required when model name is provided",
		},
		{
			name: "invalid model provider",
			cfg: &InitCfg{
				Framework:     "adk",
				Language:      "python",
				AgentName:     "test_agent",
				ModelProvider: "InvalidProvider",
			},
			wantErr: true,
			errMsg:  "unsupported model provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InitCmd(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitCmd() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("InitCmd() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}
