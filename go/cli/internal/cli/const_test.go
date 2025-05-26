package cli

import (
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
)

func TestGetModelProvider(t *testing.T) {
	testCases := []struct {
		name            string
		envVarValue     string
		expectedResult  v1alpha1.ModelProvider
		expectedAPIKey  string
		expectedHelmKey string
	}{
		{
			name:            "DefaultModelProvider when env var not set",
			envVarValue:     "",
			expectedResult:  DefaultModelProvider,
			expectedAPIKey:  OPENAI_API_KEY,
			expectedHelmKey: "openAI",
		},
		{
			name:            "OpenAI provider",
			envVarValue:     string(v1alpha1.OpenAI),
			expectedResult:  v1alpha1.OpenAI,
			expectedAPIKey:  OPENAI_API_KEY,
			expectedHelmKey: "openAI",
		},
		{
			name:            "AzureOpenAI provider",
			envVarValue:     string(v1alpha1.AzureOpenAI),
			expectedResult:  v1alpha1.AzureOpenAI,
			expectedAPIKey:  AZUREOPENAI_API_KEY,
			expectedHelmKey: "azureOpenAI",
		},
		{
			name:            "Anthropic provider",
			envVarValue:     string(v1alpha1.Anthropic),
			expectedResult:  v1alpha1.Anthropic,
			expectedAPIKey:  ANTHROPIC_API_KEY, // Changed from literal string for consistency
			expectedHelmKey: "anthropic",
		},
		{
			name:            "Ollama provider",
			envVarValue:     string(v1alpha1.Ollama),
			expectedResult:  v1alpha1.Ollama,
			expectedAPIKey:  "",
			expectedHelmKey: "ollama",
		},
		{
			name:            "Gemini provider", // Add this test case
			envVarValue:     string(v1alpha1.Gemini),
			expectedResult:  v1alpha1.Gemini,
			expectedAPIKey:  GEMINI_API_KEY,
			expectedHelmKey: "gemini",
		},
		{
			name:            "Invalid provider",
			envVarValue:     "InvalidProvider",
			expectedResult:  DefaultModelProvider,
			expectedAPIKey:  OPENAI_API_KEY, // Example for testing unrelated API key
			expectedHelmKey: "openAI",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set the environment variable if a value is provided, otherwise unset
			if tc.envVarValue != "" {
				os.Setenv(KAGENT_DEFAULT_MODEL_PROVIDER, tc.envVarValue) // Use tc.envVarValue directly as it represents the ModelProvider string
				defer os.Unsetenv(KAGENT_DEFAULT_MODEL_PROVIDER)
			} else {
				os.Unsetenv(KAGENT_DEFAULT_MODEL_PROVIDER)
			}

			result := GetModelProvider()
			if result != tc.expectedResult {
				t.Errorf("expected GetModelProvider() to return %v, got %v", tc.expectedResult, result)
			}

			apiKey := GetProviderAPIKey(tc.expectedResult)
			if apiKey != tc.expectedAPIKey {
				t.Errorf("expected GetProviderAPIKey(%v) to return %v, got %v", tc.expectedResult, tc.expectedAPIKey, apiKey)
			}

			helmKey := GetModelProviderHelmValuesKey(tc.expectedResult)
			if helmKey != tc.expectedHelmKey {
				t.Errorf("expected GetModelProviderHelmValuesKey(%v) to return %v, got %v", tc.expectedResult, tc.expectedHelmKey, helmKey)
			}
		})
	}
}
