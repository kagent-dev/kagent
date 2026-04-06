package cli

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/cli/internal/profiles"
	"github.com/stretchr/testify/assert"
)

func TestResolveInstallProfile(t *testing.T) {
	t.Run("empty profile remains empty", func(t *testing.T) {
		assert.Equal(t, "", resolveInstallProfile(""))
	})

	t.Run("valid profile is preserved", func(t *testing.T) {
		assert.Equal(t, profiles.ProfileMinimal, resolveInstallProfile(" minimal "))
	})

	t.Run("invalid profile falls back to demo", func(t *testing.T) {
		assert.Equal(t, profiles.ProfileDemo, resolveInstallProfile("unknown"))
	})
}

func TestShouldRequireProviderCredentials(t *testing.T) {
	tests := []struct {
		name          string
		profile       string
		modelProvider v1alpha2.ModelProvider
		want          bool
	}{
		{
			name:          "default install requires credentials for openai",
			profile:       "",
			modelProvider: v1alpha2.ModelProviderOpenAI,
			want:          true,
		},
		{
			name:          "minimal install skips credentials for openai",
			profile:       profiles.ProfileMinimal,
			modelProvider: v1alpha2.ModelProviderOpenAI,
			want:          false,
		},
		{
			name:          "demo install still requires credentials for anthropic",
			profile:       profiles.ProfileDemo,
			modelProvider: v1alpha2.ModelProviderAnthropic,
			want:          true,
		},
		{
			name:          "ollama never requires credentials",
			profile:       profiles.ProfileDemo,
			modelProvider: v1alpha2.ModelProviderOllama,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldRequireProviderCredentials(tt.profile, tt.modelProvider))
		})
	}
}
