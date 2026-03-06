package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAndLoadProject(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *DeployCfg
		setup   func(t *testing.T) string
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing project directory",
			cfg: &DeployCfg{
				ProjectDir: "",
			},
			setup:   nil,
			wantErr: true,
			errMsg:  "project directory is required",
		},
		{
			name: "non-existent project directory",
			cfg: &DeployCfg{
				ProjectDir: "/nonexistent/path",
			},
			setup:   nil,
			wantErr: true,
			errMsg:  "project directory does not exist",
		},
		{
			name: "missing kagent.yaml",
			cfg:  &DeployCfg{},
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
			errMsg:  "failed to load kagent.yaml",
		},
		{
			name: "valid project with manifest",
			cfg:  &DeployCfg{},
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				manifestPath := filepath.Join(tmpDir, "kagent.yaml")
				manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
`
				err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
				require.NoError(t, err)
				return tmpDir
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.cfg.ProjectDir = tt.setup(t)
			}

			manifest, err := validateAndLoadProject(tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, manifest)
			}
		})
	}
}

func TestDeployCfg_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *DeployCfg
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty project directory",
			cfg: &DeployCfg{
				ProjectDir: "",
				Config:     &config.Config{},
			},
			wantErr: true,
			errMsg:  "project directory is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateAndLoadProject(tt.cfg)
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

func TestIsVerbose(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *config.Config
		want   bool
	}{
		{
			name:   "nil config",
			cfg:    nil,
			want:   false,
		},
		{
			name:   "verbose false",
			cfg:    &config.Config{Verbose: false},
			want:   false,
		},
		{
			name:   "verbose true",
			cfg:    &config.Config{Verbose: true},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsVerbose(tt.cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}
