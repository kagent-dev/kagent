package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func checkNpxInstalled() error {
	cmd := exec.Command("npx", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npx is required to run the modelcontextinstaller. Please install Node.js and npm to get npx")
	}
	return nil
}

// createMCPInspectorConfig creates an MCP inspector configuration file
func createMCPInspectorConfig(serverName string, serverConfig map[string]any, configPath string) error {
	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	config := map[string]any{
		"mcpServers": map[string]any{
			serverName: serverConfig,
		},
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write mcp-server-config.json: %w", err)
	}

	if cfg.Verbose {
		fmt.Printf("Created mcp-server-config.json: %s\n", configPath)
		fmt.Printf("Config content:\n%s\n", string(configData))
	}

	// Check if this is a streamable-http configuration and notify user
	if serverConfig["type"] == "streamable-http" {
		fmt.Println("\nNOTE: Due to a known issue with the MCP Inspector, you will need to")
		fmt.Println("manually configure the connection in the UI:")
		fmt.Println("1. Set Transport Type to 'Streamable HTTP'")
		fmt.Println("2. Set URL to 'http://localhost:3000/mcp'")
		fmt.Println("3. Click 'Connect'")
		fmt.Printf("\n🚀 Starting MCP Inspector...\n")
	}

	return nil
}

// runMCPInspector runs the MCP inspector with the given configuration
func runMCPInspector(configPath, serverName string, workingDir string) error {
	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	args := []string{
		"@modelcontextprotocol/inspector",
		"--config", configPath,
		"--server", serverName,
	}

	if cfg.Verbose {
		fmt.Printf("Running: npx %s\n", args)
	}

	cmd := exec.Command("npx", args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run synchronously
	return cmd.Run()
}
