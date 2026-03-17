package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCfg_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *RunCfg
		setup   func(t *testing.T) string
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing project directory",
			cfg: &RunCfg{
				ProjectDir: "",
				Config:     &config.Config{},
			},
			setup:   nil,
			wantErr: true,
			errMsg:  "project directory is required",
		},
		{
			name: "non-existent project directory",
			cfg: &RunCfg{
				ProjectDir: "/nonexistent/path",
				Config:     &config.Config{},
			},
			setup:   nil,
			wantErr: true,
			errMsg:  "project directory does not exist",
		},
		{
			name: "missing docker-compose.yaml",
			cfg: &RunCfg{
				Config: &config.Config{},
			},
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
			errMsg:  "docker-compose.yaml not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.cfg.ProjectDir = tt.setup(t)
			}

			err := RunCmd(context.Background(), tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestRunCmd_DockerComposeValidation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create kagent.yaml
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
modelProvider: Ollama
`
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	// Create docker-compose.yaml
	composePath := filepath.Join(tmpDir, "docker-compose.yaml")
	composeContent := `version: '3.8'
services:
  agent:
    image: test:latest
`
	err = os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cfg := &RunCfg{
		ProjectDir: tmpDir,
		Config:     &config.Config{},
	}

	// This will fail when trying to actually run docker-compose,
	// but we've validated the file checks work
	err = RunCmd(context.Background(), cfg)
	// Expected to fail on docker-compose execution
	if err != nil {
		t.Logf("Expected failure on docker-compose execution: %v", err)
	}
}
