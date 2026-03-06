package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCmd_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *BuildCfg
		setup   func(t *testing.T) string // Returns temp dir path
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing project directory",
			cfg: &BuildCfg{
				ProjectDir: "",
				Config:     &config.Config{},
			},
			setup:   func(t *testing.T) string { return "" },
			wantErr: true,
			errMsg:  "project directory is required",
		},
		{
			name: "non-existent project directory",
			cfg: &BuildCfg{
				ProjectDir: "/nonexistent/path",
				Config:     &config.Config{},
			},
			setup:   func(t *testing.T) string { return "" },
			wantErr: true,
			errMsg:  "project directory does not exist",
		},
		{
			name: "missing Dockerfile",
			cfg: &BuildCfg{
				Config: &config.Config{},
			},
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				return tmpDir
			},
			wantErr: true,
			errMsg:  "dockerfile not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				dir := tt.setup(t)
				if dir != "" && tt.cfg.ProjectDir == "" {
					tt.cfg.ProjectDir = dir
				}
			}

			err := BuildCmd(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConstructImageName(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *BuildCfg
		setupFile func(t *testing.T, dir string) // Create kagent.yaml if needed
		want      string
	}{
		{
			name: "custom image name provided",
			cfg: &BuildCfg{
				Image:      "myregistry/myagent:v1.0",
				ProjectDir: "",
			},
			setupFile: nil,
			want:      "myregistry/myagent:v1.0",
		},
		{
			name: "fallback to directory name",
			cfg: &BuildCfg{
				Image:      "",
				ProjectDir: "/path/to/my-agent",
			},
			setupFile: nil,
			want:      "localhost:5001/my-agent:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupFile != nil {
				tmpDir := t.TempDir()
				tt.cfg.ProjectDir = tmpDir
				tt.setupFile(t, tmpDir)
			}

			got := constructImageName(tt.cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetAgentNameFromManifest(t *testing.T) {
	tests := []struct {
		name         string
		manifestYAML string
		want         string
	}{
		{
			name: "valid manifest with agent name",
			manifestYAML: `agentName: test-agent
description: Test agent
framework: adk
language: python
`,
			want: "test-agent",
		},
		{
			name:         "no manifest file",
			manifestYAML: "",
			want:         "",
		},
		{
			name: "invalid yaml",
			manifestYAML: `invalid: yaml: content:
  - broken`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if tt.manifestYAML != "" {
				manifestPath := filepath.Join(tmpDir, "kagent.yaml")
				err := os.WriteFile(manifestPath, []byte(tt.manifestYAML), 0644)
				require.NoError(t, err)
			}

			got := getAgentNameFromManifest(tmpDir)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConstructMcpServerImageName(t *testing.T) {
	tests := []struct {
		name       string
		projectDir string
		serverName string
		want       string
	}{
		{
			name:       "basic mcp server image",
			projectDir: "/path/to/my-agent",
			serverName: "weather-server",
			want:       "localhost:5001/my-agent-weather-server:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &BuildCfg{
				ProjectDir: tt.projectDir,
			}

			got := constructMcpServerImageName(cfg, tt.serverName)
			assert.Equal(t, tt.want, got)
		})
	}
}
