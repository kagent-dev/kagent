package runner

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
)

// TestCreateRunnerConfig_WiresArtifactService verifies that CreateRunnerConfig
// sets a non-nil ArtifactService so agents/tools get a working ctx.Artifacts()
// (AC1).
func TestCreateRunnerConfig_WiresArtifactService(t *testing.T) {
	tests := []struct {
		name    string
		appName string
	}{
		{name: "named app", appName: "my-app"},
		{name: "default app", appName: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", "test-key")
			ctx := logr.NewContext(t.Context(), logr.Discard())
			agentConfig := &adk.AgentConfig{
				Model: &adk.OpenAI{
					BaseModel: adk.BaseModel{Type: "openai", Model: "gpt-4o-mini"},
					BaseUrl:   "https://api.openai.com/v1",
				},
				Description: "test agent",
				Instruction: "you are helpful",
			}

			cfg, _, err := CreateRunnerConfig(ctx, agentConfig, nil, tt.appName, nil)
			if err != nil {
				t.Fatalf("CreateRunnerConfig() error = %v", err)
			}
			if cfg.ArtifactService == nil {
				t.Fatal("CreateRunnerConfig() ArtifactService = nil, want non-nil")
			}
		})
	}
}
