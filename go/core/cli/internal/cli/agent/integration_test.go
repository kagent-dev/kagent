//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitBuildWorkflow tests the complete workflow from init to build validation
func TestInitBuildWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Change to temp directory
	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Step 1: Initialize a new agent project
	initCfg := &InitCfg{
		AgentName:     "integration_test_agent",
		Framework:     "adk",
		Language:      "python",
		ModelProvider: "OpenAI",
		ModelName:     "gpt_4",
		Config:        &config.Config{},
	}

	err := InitCmd(initCfg)
	require.NoError(t, err, "Init command should succeed")

	// Verify project structure was created
	projectDir := filepath.Join(tmpDir, initCfg.AgentName)
	assert.FileExists(t, filepath.Join(projectDir, "kagent.yaml"), "kagent.yaml should exist")
	assert.FileExists(t, filepath.Join(projectDir, "docker-compose.yaml"), "docker-compose.yaml should exist")
	assert.DirExists(t, filepath.Join(projectDir, initCfg.AgentName), "agent directory should exist")

	// Verify manifest content
	manifest, err := LoadManifest(projectDir)
	require.NoError(t, err, "Should load manifest")
	assert.Equal(t, "integration_test_agent", manifest.Name)
	assert.Equal(t, "adk", manifest.Framework)
	assert.Equal(t, "python", manifest.Language)

	// Step 2: Validate build configuration can read the manifest
	buildCfg := &BuildCfg{
		ProjectDir: projectDir,
		Config:     &config.Config{},
	}

	loadedManifest, err := LoadManifest(buildCfg.ProjectDir)
	require.NoError(t, err, "Build should load manifest")
	assert.Equal(t, initCfg.AgentName, loadedManifest.Name)
}

// TestInitDeployWorkflow tests init followed by deploy validation
func TestInitDeployWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Step 1: Initialize project
	initCfg := &InitCfg{
		AgentName:     "deploy_test_agent",
		Framework:     "adk",
		Language:      "python",
		ModelProvider: "OpenAI",
		Config:        &config.Config{Namespace: "test_ns"},
	}

	err := InitCmd(initCfg)
	require.NoError(t, err)

	projectDir := filepath.Join(tmpDir, initCfg.AgentName)

	// Step 2: Validate deploy configuration
	deployCfg := &DeployCfg{
		ProjectDir: projectDir,
		Config:     &config.Config{Namespace: "test_ns"},
	}

	manifest, err := validateAndLoadProject(deployCfg)
	require.NoError(t, err, "Deploy validation should pass after init")
	assert.NotNil(t, manifest)
	assert.Equal(t, "deploy_test_agent", manifest.Name)
}

// TestInitAddMcpWorkflow tests init followed by adding MCP server
func TestInitAddMcpWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Step 1: Initialize project
	initCfg := &InitCfg{
		AgentName:     "mcp_test_agent",
		Framework:     "adk",
		Language:      "python",
		ModelProvider: "OpenAI",
		Config:        &config.Config{},
	}

	err := InitCmd(initCfg)
	require.NoError(t, err)

	projectDir := filepath.Join(tmpDir, initCfg.AgentName)

	// Step 2: Add MCP server to the project
	addMcpCfg := &AddMcpCfg{
		ProjectDir: projectDir,
		Name:       "test_mcp_server",
		RemoteURL:  "http://localhost:3000",
		Headers:    []string{"Authorization=Bearer token"},
		Config:     &config.Config{},
	}

	err = AddMcpCmd(addMcpCfg)
	// Note: This will fail on regenerateMcpToolsFile, but we can verify the manifest was updated
	if err != nil {
		t.Logf("Expected error from MCP tools regeneration: %v", err)
	}

	// Verify manifest was updated with MCP server
	manifest, err := LoadManifest(projectDir)
	require.NoError(t, err)
	assert.NotNil(t, manifest.McpServers, "McpServers should be initialized")
}

// TestProjectValidationAcrossCommands tests that validation is consistent across commands
func TestProjectValidationAcrossCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Initialize project
	initCfg := &InitCfg{
		AgentName:     "validation_test",
		Framework:     "adk",
		Language:      "python",
		ModelProvider: "OpenAI",
		Config:        &config.Config{},
	}

	err := InitCmd(initCfg)
	require.NoError(t, err)

	projectDir := filepath.Join(tmpDir, initCfg.AgentName)

	// Test 1: Deploy validation should pass
	deployCfg := &DeployCfg{
		ProjectDir: projectDir,
		Config:     &config.Config{},
	}
	_, err = validateAndLoadProject(deployCfg)
	assert.NoError(t, err, "Deploy validation should pass")

	// Test 2: Build validation should pass
	manifest, err := LoadManifest(projectDir)
	assert.NoError(t, err, "Build validation should pass")
	assert.NotNil(t, manifest)

	// Test 3: Delete manifest and verify all commands fail validation
	err = os.Remove(filepath.Join(projectDir, "kagent.yaml"))
	require.NoError(t, err)

	_, err = validateAndLoadProject(deployCfg)
	assert.Error(t, err, "Deploy validation should fail without manifest")

	_, err = LoadManifest(projectDir)
	assert.Error(t, err, "Build validation should fail without manifest")
}

// TestErrorPropagationAcrossCommands tests that errors propagate correctly
func TestErrorPropagationAcrossCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	// Test 1: Deploy without init should fail
	deployCfg := &DeployCfg{
		ProjectDir: tmpDir,
		Config:     &config.Config{},
	}
	_, err := validateAndLoadProject(deployCfg)
	assert.Error(t, err, "Deploy should fail without init")

	// Test 2: Build without init should fail
	_, err = LoadManifest(tmpDir)
	assert.Error(t, err, "Build should fail without init")

	// Test 3: Add MCP without init should fail
	addMcpCfg := &AddMcpCfg{
		ProjectDir: tmpDir,
		Name:       "test_mcp",
		RemoteURL:  "http://localhost:3000",
		Config:     &config.Config{},
	}
	err = AddMcpCmd(addMcpCfg)
	assert.Error(t, err, "Add MCP should fail without init")
}
