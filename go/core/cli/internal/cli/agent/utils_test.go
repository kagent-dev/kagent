package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadManifest(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		wantErr   bool
		wantName  string
	}{
		{
			name: "missing kagent.yaml",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: true,
		},
		{
			name: "valid kagent.yaml",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
`
				err := writeFile(tmpDir, "kagent.yaml", manifestContent)
				if err != nil {
					t.Fatal(err)
				}
				return tmpDir
			},
			wantErr:  false,
			wantName: "test-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := tt.setup(t)

			manifest, err := LoadManifest(projectDir)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manifest)
				if tt.wantName != "" {
					assert.Equal(t, tt.wantName, manifest.Name)
				}
			}
		})
	}
}

// Helper function for tests
func writeFile(dir, filename, content string) error {
	return os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644)
}
