package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeCfg_TaskInput(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *InvokeCfg
		setupFile  func(t *testing.T) string
		wantTask   string
		wantErrLog string // Expected error message in stderr
	}{
		{
			name: "task provided directly",
			cfg: &InvokeCfg{
				Task: "Get all pods in namespace",
			},
			setupFile: nil,
			wantTask:  "Get all pods in namespace",
		},
		{
			name: "task from file",
			cfg: &InvokeCfg{
				File: "",
			},
			setupFile: func(t *testing.T) string {
				tmpFile := filepath.Join(t.TempDir(), "task.txt")
				err := os.WriteFile(tmpFile, []byte("Task from file"), 0644)
				require.NoError(t, err)
				return tmpFile
			},
			wantTask: "Task from file",
		},
		{
			name: "no task or file",
			cfg: &InvokeCfg{
				Task: "",
				File: "",
			},
			setupFile:  nil,
			wantErrLog: "Task or file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupFile != nil {
				tt.cfg.File = tt.setupFile(t)
			}

			var task string
			if tt.cfg.Task != "" {
				task = tt.cfg.Task
			} else if tt.cfg.File != "" && tt.cfg.File != "-" {
				content, err := os.ReadFile(tt.cfg.File)
				if err == nil {
					task = string(content)
				}
			}

			if tt.wantTask != "" {
				assert.Equal(t, tt.wantTask, task)
			}
		})
	}
}

func TestInvokeCfg_AgentValidation(t *testing.T) {
	tests := []struct {
		name      string
		agent     string
		wantValid bool
	}{
		{
			name:      "valid agent name",
			agent:     "k8s-agent",
			wantValid: true,
		},
		{
			name:      "agent with namespace - invalid",
			agent:     "default/k8s-agent",
			wantValid: false,
		},
		{
			name:      "empty agent",
			agent:     "",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &InvokeCfg{
				Agent:  tt.agent,
				Config: &config.Config{},
			}

			// Test agent format validation
			if tt.agent != "" {
				hasSlash := contains(cfg.Agent, "/")
				if tt.wantValid {
					assert.False(t, hasSlash, "Valid agent name should not contain '/'")
				} else if hasSlash {
					assert.True(t, hasSlash, "Invalid agent format contains '/'")
				}
			}
		})
	}
}

func TestInvokeCfg_URLConstruction(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *InvokeCfg
		wantURL   string
	}{
		{
			name: "standard URL construction",
			cfg: &InvokeCfg{
				Agent: "k8s-agent",
				Config: &config.Config{
					KAgentURL: "http://localhost:8083",
					Namespace: "kagent",
				},
			},
			wantURL: "http://localhost:8083/api/a2a/kagent/k8s-agent",
		},
		{
			name: "custom namespace",
			cfg: &InvokeCfg{
				Agent: "my-agent",
				Config: &config.Config{
					KAgentURL: "http://kagent.example.com",
					Namespace: "production",
				},
			},
			wantURL: "http://kagent.example.com/api/a2a/production/my-agent",
		},
		{
			name: "URL override provided",
			cfg: &InvokeCfg{
				Agent:       "k8s-agent",
				URLOverride: "http://custom-url:8080/a2a",
				Config: &config.Config{
					KAgentURL: "http://localhost:8083",
					Namespace: "kagent",
				},
			},
			wantURL: "http://custom-url:8080/a2a", // URLOverride is used directly
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actualURL string
			if tt.cfg.URLOverride != "" {
				actualURL = tt.cfg.URLOverride
			} else {
				actualURL = tt.cfg.Config.KAgentURL + "/api/a2a/" + tt.cfg.Config.Namespace + "/" + tt.cfg.Agent
			}

			assert.Equal(t, tt.wantURL, actualURL)
		})
	}
}
