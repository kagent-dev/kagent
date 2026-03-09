package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/manifests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjectDir(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *RunCfg
		wantErr   bool
		checkFunc func(t *testing.T, dir string)
	}{
		{
			name:    "use current directory when not specified",
			cfg:     &RunCfg{ProjectDir: ""},
			wantErr: false,
			checkFunc: func(t *testing.T, dir string) {
				assert.NotEmpty(t, dir)
				assert.True(t, filepath.IsAbs(dir))
			},
		},
		{
			name:    "use specified absolute path",
			cfg:     &RunCfg{ProjectDir: "/tmp/test-project"},
			wantErr: false,
			checkFunc: func(t *testing.T, dir string) {
				assert.Equal(t, "/tmp/test-project", dir)
			},
		},
		{
			name:    "convert relative path to absolute",
			cfg:     &RunCfg{ProjectDir: "./test-project"},
			wantErr: false,
			checkFunc: func(t *testing.T, dir string) {
				assert.True(t, filepath.IsAbs(dir))
				assert.Contains(t, dir, "test-project")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := getProjectDir(tt.cfg)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkFunc != nil {
					tt.checkFunc(t, dir)
				}
			}
		})
	}
}

func TestGetProjectManifest(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr bool
		errMsg  string
	}{
		{
			name: "manifest exists and valid",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				manifestPath := filepath.Join(tmpDir, "manifest.yaml")
				content := `name: test-server
framework: fastmcp-python
version: 1.0.0
description: Test MCP server
tools: {}
secrets: {}
`
				err := os.WriteFile(manifestPath, []byte(content), 0644)
				require.NoError(t, err)
				return tmpDir
			},
			wantErr: false,
		},
		{
			name: "manifest missing",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
			errMsg:  "manifest.yaml not found",
		},
		{
			name: "invalid manifest format",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				manifestPath := filepath.Join(tmpDir, "manifest.yaml")
				content := `name: test-server
framework: [invalid yaml
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
			projectDir := tt.setup(t)

			manifest, err := getProjectManifest(projectDir)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manifest)
			}
		})
	}
}

func TestRunCmd_Flags(t *testing.T) {
	cmd := newRunCmd()

	tests := []struct {
		name         string
		flagName     string
		expectedType string
	}{
		{name: "project-dir flag", flagName: "project-dir", expectedType: "string"},
		{name: "no-inspector flag", flagName: "no-inspector", expectedType: "bool"},
		{name: "transport flag", flagName: "transport", expectedType: "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag %s should exist", tt.flagName)
			assert.Equal(t, tt.expectedType, flag.Value.Type())
		})
	}
}

func TestRunCmd_TransportDefault(t *testing.T) {
	cmd := newRunCmd()

	flag := cmd.Flags().Lookup("transport")
	require.NotNil(t, flag)
	assert.Equal(t, "stdio", flag.DefValue)
}

func TestRunMcp_MissingManifest(t *testing.T) {
	cfg := &RunCfg{
		ProjectDir: t.TempDir(),
	}

	err := RunMcp(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.yaml not found")
}

func TestRunMcp_UnsupportedFramework(t *testing.T) {
	tmpDir := t.TempDir()

	// Create manifest with unsupported framework
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")
	content := `name: test-server
framework: unsupported-framework
version: 1.0.0
description: Test MCP server
tools: {}
secrets: {}
`
	err := os.WriteFile(manifestPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg := &RunCfg{
		ProjectDir: tmpDir,
	}

	err = RunMcp(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported framework")
}

func TestGetProjectManifest_ValidFrameworks(t *testing.T) {
	frameworks := []string{
		"fastmcp-python",
		"mcp-go",
		"typescript",
		"java",
	}

	for _, framework := range frameworks {
		t.Run(framework, func(t *testing.T) {
			tmpDir := t.TempDir()
			manifestPath := filepath.Join(tmpDir, "manifest.yaml")
			content := `name: test-server
framework: ` + framework + `
version: 1.0.0
description: Test MCP server
tools: {}
secrets: {}
`
			err := os.WriteFile(manifestPath, []byte(content), 0644)
			require.NoError(t, err)

			manifest, err := getProjectManifest(tmpDir)
			assert.NoError(t, err)
			assert.Equal(t, framework, manifest.Framework)
		})
	}
}

func TestRunCmd_NoInspectorFlag(t *testing.T) {
	cfg := &RunCfg{}

	cfg.NoInspector = true
	assert.True(t, cfg.NoInspector)

	cfg.NoInspector = false
	assert.False(t, cfg.NoInspector)
}

func TestManifestValidation(t *testing.T) {
	tests := []struct {
		name         string
		manifestYAML string
		wantErr      bool
		checkFunc    func(t *testing.T, m *manifests.ProjectManifest)
	}{
		{
			name: "minimal valid manifest",
			manifestYAML: `name: test-server
framework: fastmcp-python
version: 1.0.0
description: Test server
tools: {}
secrets: {}
`,
			wantErr: false,
			checkFunc: func(t *testing.T, m *manifests.ProjectManifest) {
				assert.Equal(t, "test-server", m.Name)
				assert.Equal(t, "fastmcp-python", m.Framework)
				assert.Equal(t, "1.0.0", m.Version)
			},
		},
		{
			name: "manifest with tools",
			manifestYAML: `name: test-server
framework: fastmcp-python
version: 1.0.0
description: Test server
tools:
  echo:
    name: echo
    description: Echo tool
    enabled: true
secrets: {}
`,
			wantErr: false,
			checkFunc: func(t *testing.T, m *manifests.ProjectManifest) {
				assert.Len(t, m.Tools, 1)
				tool, exists := m.Tools["echo"]
				assert.True(t, exists)
				assert.Equal(t, "echo", tool.Name)
			},
		},
		{
			name: "manifest with secrets",
			manifestYAML: `name: test-server
framework: fastmcp-python
version: 1.0.0
description: Test server
tools: {}
secrets:
  API_KEY:
    enabled: true
    provider: env
`,
			wantErr: false,
			checkFunc: func(t *testing.T, m *manifests.ProjectManifest) {
				assert.Len(t, m.Secrets, 1)
				secret, exists := m.Secrets["API_KEY"]
				assert.True(t, exists)
				assert.True(t, secret.Enabled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			manifestPath := filepath.Join(tmpDir, "manifest.yaml")
			err := os.WriteFile(manifestPath, []byte(tt.manifestYAML), 0644)
			require.NoError(t, err)

			manifest, err := getProjectManifest(tmpDir)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkFunc != nil {
					tt.checkFunc(t, manifest)
				}
			}
		})
	}
}
