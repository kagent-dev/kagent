package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	commonexec "github.com/kagent-dev/kagent/go/cli/internal/common/exec"
	commonimage "github.com/kagent-dev/kagent/go/cli/internal/common/image"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

type BuildCfg struct {
	ProjectDir     string
	Image          string
	Push           bool
	Platform       string
	Config         *config.Config
	SkipMCPServers bool
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
		return fmt.Errorf("dockerfile not found in project directory: %s", dockerfilePath)
	}

	// Check if Docker is available and running
	docker := commonexec.NewDockerExecutor(cfg.Config.Verbose, cfg.ProjectDir)
	if err := docker.CheckAvailability(); err != nil {
		return fmt.Errorf("docker check failed: %v", err)
	}

	// Load the manifest to check for MCP servers
	manifest := getManifestFromProjectDir(cfg.ProjectDir)
	if manifest != nil && len(manifest.McpServers) > 0 {
		// Regenerate mcp_tools.py to ensure it's up-to-date before building
		if err := regenerateMcpToolsFile(cfg.ProjectDir, manifest, cfg.Config.Verbose); err != nil {
			return fmt.Errorf("failed to regenerate mcp_tools.py: %v", err)
		}
	}

	imageName := constructImageName(cfg)
	var extraArgs []string
	if cfg.Platform != "" {
		extraArgs = append(extraArgs, "--platform", cfg.Platform)
	}
	if err := docker.Build(imageName, ".", extraArgs...); err != nil {
		return fmt.Errorf("failed to build Docker image: %v", err)
	}

	if cfg.Push {
		if err := docker.Push(imageName); err != nil {
			return fmt.Errorf("failed to push Docker image: %v", err)
		}
	}

	// Check if MCP servers exist and build images for each MCP server
	// Skip if SkipMCPServers flag is set
	if !cfg.SkipMCPServers && manifest != nil && len(manifest.McpServers) > 0 {
		if err := buildMcpServerImages(cfg, manifest); err != nil {
			return fmt.Errorf("failed to build MCP server images: %v", err)
		}

		// Push the MCP server images if requested
		if cfg.Push {
			if err := pushMcpServerImages(cfg, manifest); err != nil {
				return fmt.Errorf("failed to push MCP server images: %v", err)
			}
		}
	}

	return nil
}

// constructImageName constructs the full image name from the provided image or defaults
func constructImageName(cfg *BuildCfg) string {
	agentName := getAgentNameFromManifest(cfg.ProjectDir)

	// If no agent name found in manifest, fall back to directory name
	if agentName == "" {
		agentName = filepath.Base(cfg.ProjectDir)
	}

	// Construct full image name using common utility
	return commonimage.ConstructImageName(cfg.Image, agentName)
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

// getManifestFromProjectDir loads the agent manifest from the project directory
func getManifestFromProjectDir(projectDir string) *common.AgentManifest {
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err != nil {
		// Silently fail and return nil
		return nil
	}
	return manifest
}

// buildMcpServerImages builds Docker images for each MCP server
func buildMcpServerImages(cfg *BuildCfg, manifest *common.AgentManifest) error {
	// Build an image for each command-type MCP server
	for _, srv := range manifest.McpServers {
		// Skip remote type servers as they don't need to be built
		if srv.Type != "command" {
			continue
		}

		mcpServerDir := filepath.Join(cfg.ProjectDir, srv.Name)
		if _, err := os.Stat(mcpServerDir); os.IsNotExist(err) {
			// Directory doesn't exist, skip building
			if cfg.Config.Verbose {
				fmt.Printf("Skipping %s: directory not found\n", srv.Name)
			}
			continue
		}

		// Check if Dockerfile exists in the MCP server directory
		dockerfilePath := filepath.Join(mcpServerDir, "Dockerfile")
		if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
			return fmt.Errorf("file Dockerfile not found in %s directory: %s", srv.Name, dockerfilePath)
		}

		// Construct the MCP server image name and build using shared executor
		imageName := constructMcpServerImageName(cfg, srv.Name)
		docker := commonexec.NewDockerExecutor(cfg.Config.Verbose, mcpServerDir)

		var extraArgs []string
		if cfg.Platform != "" {
			extraArgs = append(extraArgs, "--platform", cfg.Platform)
		}
		if err := docker.Build(imageName, ".", extraArgs...); err != nil {
			return fmt.Errorf("docker build failed for %s: %v", srv.Name, err)
		}
	}

	return nil
}

// pushMcpServerImages pushes the MCP server Docker images to the specified registry
func pushMcpServerImages(cfg *BuildCfg, manifest *common.AgentManifest) error {
	docker := commonexec.NewDockerExecutor(cfg.Config.Verbose, "")

	// Push an image for each command-type MCP server
	for _, srv := range manifest.McpServers {
		// Skip remote type servers
		if srv.Type != "command" {
			continue
		}

		mcpServerDir := filepath.Join(cfg.ProjectDir, srv.Name)
		if _, err := os.Stat(mcpServerDir); os.IsNotExist(err) {
			// Directory doesn't exist, skip pushing
			if cfg.Config.Verbose {
				fmt.Printf("Skipping %s: directory not found\n", srv.Name)
			}
			continue
		}

		imageName := constructMcpServerImageName(cfg, srv.Name)

		if err := docker.Push(imageName); err != nil {
			return fmt.Errorf("docker push failed for %s: %v", srv.Name, err)
		}
	}

	return nil
}

// constructMcpServerImageName constructs the MCP server image name
func constructMcpServerImageName(cfg *BuildCfg, serverName string) string {
	// Get agent name from kagent.yaml file
	agentName := getAgentNameFromManifest(cfg.ProjectDir)

	// If no agent name found in manifest, fall back to directory name
	if agentName == "" {
		agentName = filepath.Base(cfg.ProjectDir)
	}
	return commonimage.ConstructMCPServerImageName(agentName, serverName)
}
