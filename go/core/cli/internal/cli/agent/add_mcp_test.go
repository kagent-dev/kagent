package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAddMcpCmd_AddRemoteServer(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial manifest without MCP servers
	manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
modelProvider: anthropic
`
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	cfg := &AddMcpCfg{
		ProjectDir: tmpDir,
		Name:       "github-server",
		RemoteURL:  "https://api.github.com/mcp",
		Headers:    []string{"Authorization=Bearer ${GITHUB_TOKEN}"},
		Config:     &config.Config{},
	}

	// Call AddMcpCmd
	err = AddMcpCmd(cfg)
	require.NoError(t, err)

	// Verify manifest was updated
	content, err := os.ReadFile(manifestPath)
	require.NoError(t, err)

	var manifest map[string]any
	err = yaml.Unmarshal(content, &manifest)
	require.NoError(t, err)

	mcpServers, ok := manifest["mcpServers"].([]any)
	require.True(t, ok, "mcpServers should be an array")
	require.Len(t, mcpServers, 1)

	server := mcpServers[0].(map[string]any)
	assert.Equal(t, "github-server", server["name"])
	assert.Equal(t, "remote", server["type"])
	assert.Equal(t, "https://api.github.com/mcp", server["url"])
}

func TestAddMcpCmd_AddCommandServer(t *testing.T) {
	tmpDir := t.TempDir()

	manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
`
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	cfg := &AddMcpCfg{
		ProjectDir: tmpDir,
		Name:       "filesystem-server",
		Command:    "npx",
		Args:       []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
		Config:     &config.Config{},
	}

	err = AddMcpCmd(cfg)
	require.NoError(t, err)

	// Verify manifest was updated
	content, err := os.ReadFile(manifestPath)
	require.NoError(t, err)

	var manifest map[string]any
	err = yaml.Unmarshal(content, &manifest)
	require.NoError(t, err)

	mcpServers := manifest["mcpServers"].([]any)
	require.Len(t, mcpServers, 1)

	server := mcpServers[0].(map[string]any)
	assert.Equal(t, "filesystem-server", server["name"])
	assert.Equal(t, "command", server["type"])
	assert.Equal(t, "npx", server["command"])
}

func TestAddMcpCmd_MissingManifest(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &AddMcpCfg{
		ProjectDir: tmpDir,
		Name:       "test-server",
		RemoteURL:  "http://example.com",
		Config:     &config.Config{},
	}

	err := AddMcpCmd(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load kagent.yaml")
}

func TestAddMcpCmd_AddMultipleServers(t *testing.T) {
	tmpDir := t.TempDir()

	manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
`
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	// Add first server (remote)
	cfg1 := &AddMcpCfg{
		ProjectDir: tmpDir,
		Name:       "server-1",
		RemoteURL:  "http://server1.com",
		Config:     &config.Config{},
	}
	err = AddMcpCmd(cfg1)
	require.NoError(t, err)

	// Add second server (command)
	cfg2 := &AddMcpCfg{
		ProjectDir: tmpDir,
		Name:       "server-2",
		Command:    "python",
		Args:       []string{"server.py"},
		Config:     &config.Config{},
	}
	err = AddMcpCmd(cfg2)
	require.NoError(t, err)

	// Verify both servers exist
	content, err := os.ReadFile(manifestPath)
	require.NoError(t, err)

	var manifest map[string]any
	err = yaml.Unmarshal(content, &manifest)
	require.NoError(t, err)

	mcpServers := manifest["mcpServers"].([]any)
	require.Len(t, mcpServers, 2)

	// Verify first server
	server1 := mcpServers[0].(map[string]any)
	assert.Equal(t, "server-1", server1["name"])

	// Verify second server
	server2 := mcpServers[1].(map[string]any)
	assert.Equal(t, "server-2", server2["name"])
}
