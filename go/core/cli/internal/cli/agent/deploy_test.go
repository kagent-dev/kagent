package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeployCmd_DryRun_Success(t *testing.T) {
	// Create temporary project directory with manifest and Dockerfile
	tmpDir := t.TempDir()

	// Create kagent.yaml
	manifestContent := `agentName: test-agent
description: Test agent for deployment
framework: adk
language: python
modelProvider: anthropic
`
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	// Create Dockerfile
	dockerfileContent := `FROM python:3.12-slim
WORKDIR /app
COPY . .
CMD ["python", "main.py"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	err = os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644)
	require.NoError(t, err)

	// Create .env file with API key
	envContent := `ANTHROPIC_API_KEY=test-key-12345
OTHER_VAR=value
`
	envPath := filepath.Join(tmpDir, ".env")
	err = os.WriteFile(envPath, []byte(envContent), 0644)
	require.NoError(t, err)

	// Setup config
	cfg := &DeployCfg{
		ProjectDir: tmpDir,
		EnvFile:    envPath,
		DryRun:     true, // Dry-run mode to avoid Docker build
		Config: &config.Config{
			Namespace: "test-namespace",
		},
	}

	// Create fake K8s client (not used in dry-run, but required by signature)
	k8sClient := testutil.NewFakeControllerClient(t)

	// Call DeployCmd
	err = DeployCmd(context.Background(), k8sClient, cfg)

	// Should succeed in dry-run mode
	assert.NoError(t, err)
}

func TestDeployCmd_MissingProjectDir(t *testing.T) {
	cfg := &DeployCfg{
		ProjectDir: "",
		Config:     &config.Config{},
	}

	k8sClient := testutil.NewFakeControllerClient(t)

	err := DeployCmd(context.Background(), k8sClient, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project directory is required")
}

func TestDeployCmd_NonExistentProjectDir(t *testing.T) {
	cfg := &DeployCfg{
		ProjectDir: "/nonexistent/path",
		Config:     &config.Config{},
	}

	k8sClient := testutil.NewFakeControllerClient(t)

	err := DeployCmd(context.Background(), k8sClient, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project directory does not exist")
}

func TestDeployCmd_MissingManifest(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &DeployCfg{
		ProjectDir: tmpDir,
		Config:     &config.Config{},
	}

	k8sClient := testutil.NewFakeControllerClient(t)

	err := DeployCmd(context.Background(), k8sClient, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load kagent.yaml")
}

func TestDeployCmd_MissingModelProvider(t *testing.T) {
	tmpDir := t.TempDir()

	// Create manifest without modelProvider
	manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
`
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	cfg := &DeployCfg{
		ProjectDir: tmpDir,
		DryRun:     true,
		Config:     &config.Config{},
	}

	k8sClient := testutil.NewFakeControllerClient(t)

	err = DeployCmd(context.Background(), k8sClient, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "model provider is required")
}

func TestDeployCmd_MissingEnvFile(t *testing.T) {
	tmpDir := t.TempDir()

	manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
modelProvider: anthropic
`
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	cfg := &DeployCfg{
		ProjectDir: tmpDir,
		EnvFile:    "", // Missing env file
		DryRun:     true,
		Config:     &config.Config{},
	}

	k8sClient := testutil.NewFakeControllerClient(t)

	err = DeployCmd(context.Background(), k8sClient, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--env-file is required")
}

func TestDeployCmd_EnvFileMissingAPIKey(t *testing.T) {
	tmpDir := t.TempDir()

	manifestContent := `agentName: test-agent
description: Test agent
framework: adk
language: python
modelProvider: anthropic
`
	manifestPath := filepath.Join(tmpDir, "kagent.yaml")
	err := os.WriteFile(manifestPath, []byte(manifestContent), 0644)
	require.NoError(t, err)

	// Create .env file WITHOUT the required API key
	envContent := `OTHER_VAR=value
SOME_KEY=some_value
`
	envPath := filepath.Join(tmpDir, ".env")
	err = os.WriteFile(envPath, []byte(envContent), 0644)
	require.NoError(t, err)

	cfg := &DeployCfg{
		ProjectDir: tmpDir,
		EnvFile:    envPath,
		DryRun:     true,
		Config:     &config.Config{},
	}

	k8sClient := testutil.NewFakeControllerClient(t)

	err = DeployCmd(context.Background(), k8sClient, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must contain ANTHROPIC_API_KEY")
}
