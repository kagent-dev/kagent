package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/frameworks/common"
)

type BuildCfg struct {
	ProjectDir string
	Image      string
	Push       bool
	Config     *config.Config
}

// BuildCmd builds a Docker image for an agent project
func BuildCmd(cfg *BuildCfg) error {
	// Validate project directory
	if cfg.ProjectDir == "" {
		return fmt.Errorf("project directory is required")
	}

	// Check if project directory exists
	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	// Check if Dockerfile exists in project directory
	dockerfilePath := filepath.Join(cfg.ProjectDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return fmt.Errorf("Dockerfile not found in project directory: %s", dockerfilePath)
	}

	// Build the Docker image
	if err := buildDockerImage(cfg); err != nil {
		return fmt.Errorf("failed to build Docker image: %v", err)
	}

	// Push the image if requested
	if cfg.Push {
		if err := pushDockerImage(cfg); err != nil {
			return fmt.Errorf("failed to push Docker image: %v", err)
		}
	}

	return nil
}

// buildDockerImage builds the Docker image using docker build
func buildDockerImage(cfg *BuildCfg) error {
	// Construct the image name
	imageName := constructImageName(cfg)

	// Build command arguments
	args := []string{"build", "-t", imageName, "."}

	// Execute docker build command
	cmd := exec.Command("docker", args...)
	cmd.Dir = cfg.ProjectDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.Config.Verbose {
		fmt.Printf("Executing: docker %s\n", strings.Join(args, " "))
		fmt.Printf("Working directory: %s\n", cmd.Dir)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %v", err)
	}

	fmt.Printf("Successfully built Docker image: %s\n", imageName)
	return nil
}

// pushDockerImage pushes the Docker image to the specified registry
func pushDockerImage(cfg *BuildCfg) error {
	// Construct the image name
	imageName := constructImageName(cfg)

	// Execute docker push command
	cmd := exec.Command("docker", "push", imageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if cfg.Config.Verbose {
		fmt.Printf("Executing: docker push %s\n", imageName)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker push failed: %v", err)
	}

	fmt.Printf("Successfully pushed Docker image: %s\n", imageName)
	return nil
}

// constructImageName constructs the full image name from the provided image or defaults
func constructImageName(cfg *BuildCfg) string {
	// If a full image specification is provided, use it as-is
	if cfg.Image != "" {
		return cfg.Image
	}

	// Otherwise, construct from defaults
	// Get agent name from kagent.yaml file
	agentName := getAgentNameFromManifest(cfg.ProjectDir)

	// If no agent name found in manifest, fall back to directory name
	if agentName == "" {
		agentName = filepath.Base(cfg.ProjectDir)
	}

	// Use default registry and tag
	registry := "localhost:5001"
	tag := "latest"

	// Construct full image name: registry/agent-name:tag
	return fmt.Sprintf("%s/%s:%s", registry, agentName, tag)
}

// getAgentNameFromManifest attempts to load the agent name from kagent.yaml
func getAgentNameFromManifest(projectDir string) string {
	// Use the Manager to load the manifest
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err != nil {
		// Silently fail and return empty string to fall back to directory name
		return ""
	}

	return manifest.Name
}
