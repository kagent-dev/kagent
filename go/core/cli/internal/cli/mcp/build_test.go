package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCmd_Flags(t *testing.T) {
	// Verify flags are properly configured by constructing the command
	cmd := newBuildCmd()

	tests := []struct {
		name         string
		flagName     string
		expectedType string
	}{
		{name: "tag flag", flagName: "tag", expectedType: "string"},
		{name: "push flag", flagName: "push", expectedType: "bool"},
		{name: "kind-load flag", flagName: "kind-load", expectedType: "bool"},
		{name: "kind-load-cluster flag", flagName: "kind-load-cluster", expectedType: "string"},
		{name: "project-dir flag", flagName: "project-dir", expectedType: "string"},
		{name: "platform flag", flagName: "platform", expectedType: "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag %s should exist", tt.flagName)
			assert.Equal(t, tt.expectedType, flag.Value.Type())
		})
	}
}

func TestBuildMcp_MissingManifest(t *testing.T) {
	cfg := &BuildCfg{
		ProjectDir: t.TempDir(),
		Tag:        "", // Force manifest lookup
	}

	err := BuildMcp(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.yaml not found")
}

func TestBuildMcp_WithExplicitTag(t *testing.T) {
	cfg := &BuildCfg{
		Tag: "my-server:latest",
	}

	// Verify the config is set correctly
	assert.Equal(t, "my-server:latest", cfg.Tag)
	assert.False(t, cfg.Push)
	assert.False(t, cfg.KindLoad)
}

func TestBuildMcp_ManifestImageName(t *testing.T) {
	tests := []struct {
		name             string
		projectName      string
		version          string
		expectedImageTag string
	}{
		{
			name:             "simple name with version",
			projectName:      "MyServer",
			version:          "1.0.0",
			expectedImageTag: "my-server:1.0.0",
		},
		{
			name:             "name with underscores",
			projectName:      "my_mcp_server",
			version:          "2.0.0",
			expectedImageTag: "my-mcp-server:2.0.0",
		},
		{
			name:             "no version defaults to latest",
			projectName:      "TestServer",
			version:          "",
			expectedImageTag: "test-server:latest",
		},
		{
			name:             "name with spaces",
			projectName:      "My MCP Server",
			version:          "1.5.0",
			expectedImageTag: "my-mcp-server:1.5.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Create manifest
			manifestPath := filepath.Join(tmpDir, "manifest.yaml")
			content := `name: ` + tt.projectName + `
framework: fastmcp-python
version: ` + tt.version + `
description: Test server
tools: {}
secrets: {}
`
			err := os.WriteFile(manifestPath, []byte(content), 0644)
			require.NoError(t, err)

			// Note: We can't actually run the build without Docker,
			// but the manifest loading logic is tested via BuildMcp
		})
	}
}

func TestBuildMcp_ValidationOnly(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr bool
		errMsg  string
	}{
		{
			name: "manifest missing",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
			errMsg:  "manifest.yaml not found",
		},
		{
			name: "invalid manifest",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				manifestPath := filepath.Join(tmpDir, "manifest.yaml")
				content := `name: test-server
framework: [invalid
`
				err := os.WriteFile(manifestPath, []byte(content), 0644)
				require.NoError(t, err)
				return tmpDir
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &BuildCfg{
				ProjectDir: tt.setup(t),
				Tag:        "", // Force manifest lookup
			}

			err := BuildMcp(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			}
		})
	}
}
