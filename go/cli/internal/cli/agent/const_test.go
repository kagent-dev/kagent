package cli

import (
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/pkg/env"
)

func TestGetModelProvider(t *testing.T) {
	testCases := []struct {
		name            string
		envVarValue     string
		expectedResult  v1alpha2.ModelProvider
		expectedAPIKey  string
		expectedHelmKey string
	}{
		{
			name:            "DefaultModelProvider when env var not set",
			envVarValue:     "",
			expectedResult:  DefaultModelProvider,
			expectedAPIKey:  env.OpenAIAPIKey.Name(),
			expectedHelmKey: "openAI",
		},
		{
			name:            "OpenAI provider",
			envVarValue:     string(v1alpha2.ModelProviderOpenAI),
			expectedResult:  v1alpha2.ModelProviderOpenAI,
			expectedAPIKey:  env.OpenAIAPIKey.Name(),
			expectedHelmKey: "openAI",
		},
		{
			name:            "AzureOpenAI provider",
			envVarValue:     string(v1alpha2.ModelProviderAzureOpenAI),
			expectedResult:  v1alpha2.ModelProviderAzureOpenAI,
			expectedAPIKey:  env.AzureOpenAIAPIKey.Name(),
			expectedHelmKey: "azureOpenAI",
		},
		{
			name:            "Anthropic provider",
			envVarValue:     string(v1alpha2.ModelProviderAnthropic),
			expectedResult:  v1alpha2.ModelProviderAnthropic,
			expectedAPIKey:  "ANTHROPIC_API_KEY",
			expectedHelmKey: "anthropic",
		},
		{
			name:            "Ollama provider",
			envVarValue:     string(v1alpha2.ModelProviderOllama),
			expectedResult:  v1alpha2.ModelProviderOllama,
			expectedAPIKey:  "",
			expectedHelmKey: "ollama",
		},
		{
			name:            "Invalid provider",
			envVarValue:     "InvalidProvider",
			expectedResult:  DefaultModelProvider,
			expectedAPIKey:  env.OpenAIAPIKey.Name(), // Example for testing unrelated API key
			expectedHelmKey: "openAI",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVarValue == "" {
				os.Unsetenv(env.KagentDefaultModelProvider.Name()) //nolint:errcheck
			} else {
				os.Setenv(env.KagentDefaultModelProvider.Name(), tc.expectedHelmKey)
				defer os.Unsetenv(env.KagentDefaultModelProvider.Name()) //nolint:errcheck
			}

			result := GetModelProvider()
			if result != tc.expectedResult {
				t.Errorf("expected %v, got %v", tc.expectedResult, result)
			}

			apiKey := GetProviderAPIKey(tc.expectedResult)
			if apiKey != tc.expectedAPIKey {
				t.Errorf("expected API key %v, got %v", tc.expectedAPIKey, apiKey)
			}

			helmKey := GetModelProviderHelmValuesKey(tc.expectedResult)
			if helmKey != tc.expectedHelmKey {
				t.Errorf("expected helm key %v, got %v", tc.expectedHelmKey, helmKey)
			}
		})
	}
}
