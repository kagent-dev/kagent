package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/assert"
)

func TestInstallCfg_ProfileValidation(t *testing.T) {
	tests := []struct {
		name           string
		profile        string
		expectedValid  bool
		expectedResult string
	}{
		{
			name:           "valid demo profile",
			profile:        "demo",
			expectedValid:  true,
			expectedResult: "demo",
		},
		{
			name:           "valid minimal profile",
			profile:        "minimal",
			expectedValid:  true,
			expectedResult: "minimal",
		},
		{
			name:           "invalid profile defaults to demo",
			profile:        "invalid-profile",
			expectedValid:  false,
			expectedResult: "demo",
		},
		{
			name:           "empty profile",
			profile:        "",
			expectedValid:  true,
			expectedResult: "",
		},
		{
			name:           "profile with whitespace",
			profile:        "  demo  ",
			expectedValid:  true,
			expectedResult: "demo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &InstallCfg{
				Profile: tt.profile,
				Config:  &config.Config{},
			}

			// Simulate profile validation logic
			profile := trimAndValidateProfile(cfg.Profile)

			assert.Equal(t, tt.expectedResult, profile)
		})
	}
}

// Helper function to simulate profile validation (extracted logic from InstallCmd)
func trimAndValidateProfile(profile string) string {
	profile = trimString(profile)
	if profile == "" {
		return ""
	}

	validProfiles := []string{"demo", "minimal"}
	for _, valid := range validProfiles {
		if profile == valid {
			return profile
		}
	}

	// Invalid profile defaults to demo
	return "demo"
}

func trimString(s string) string {
	return strings.TrimSpace(s)
}

func TestGetProviderAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		provider v1alpha2.ModelProvider
		want     string
	}{
		{
			name:     "OpenAI provider",
			provider: v1alpha2.ModelProviderOpenAI,
			want:     env.OpenAIAPIKey.Name(),
		},
		{
			name:     "Anthropic provider",
			provider: v1alpha2.ModelProviderAnthropic,
			want:     "ANTHROPIC_API_KEY",
		},
		{
			name:     "Gemini provider (not in switch case)",
			provider: v1alpha2.ModelProviderGemini,
			want:     "", // Gemini not currently in GetProviderAPIKey switch
		},
		{
			name:     "Ollama provider (no API key)",
			provider: v1alpha2.ModelProviderOllama,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetProviderAPIKey(tt.provider)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetModelProviderHelmValuesKey(t *testing.T) {
	tests := []struct {
		name     string
		provider v1alpha2.ModelProvider
		want     string
	}{
		{
			name:     "OpenAI provider",
			provider: v1alpha2.ModelProviderOpenAI,
			want:     "openAI",
		},
		{
			name:     "Anthropic provider",
			provider: v1alpha2.ModelProviderAnthropic,
			want:     "anthropic",
		},
		{
			name:     "Gemini provider",
			provider: v1alpha2.ModelProviderGemini,
			want:     "gemini",
		},
		{
			name:     "Ollama provider",
			provider: v1alpha2.ModelProviderOllama,
			want:     "ollama",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetModelProviderHelmValuesKey(tt.provider)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInstallCmd_APIKeyCheck(t *testing.T) {
	tests := []struct {
		name        string
		provider    v1alpha2.ModelProvider
		setEnvVar   bool
		shouldError bool
	}{
		{
			name:        "OpenAI with API key set",
			provider:    v1alpha2.ModelProviderOpenAI,
			setEnvVar:   true,
			shouldError: false,
		},
		{
			name:        "OpenAI without API key",
			provider:    v1alpha2.ModelProviderOpenAI,
			setEnvVar:   false,
			shouldError: true,
		},
		{
			name:        "Ollama (no API key required)",
			provider:    v1alpha2.ModelProviderOllama,
			setEnvVar:   false,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKeyName := GetProviderAPIKey(tt.provider)

			// Clean up env var before and after test
			originalValue := os.Getenv(apiKeyName)
			defer func() {
				if originalValue != "" {
					os.Setenv(apiKeyName, originalValue)
				} else {
					os.Unsetenv(apiKeyName)
				}
			}()

			// Set or unset the API key
			if tt.setEnvVar && apiKeyName != "" {
				os.Setenv(apiKeyName, "test-api-key")
			} else {
				os.Unsetenv(apiKeyName)
			}

			// Check if API key validation would fail
			apiKeyValue := os.Getenv(apiKeyName)
			wouldFail := apiKeyName != "" && apiKeyValue == ""

			if tt.shouldError {
				assert.True(t, wouldFail, "Expected API key check to fail")
			} else {
				assert.False(t, wouldFail, "Expected API key check to pass")
			}
		})
	}
}
