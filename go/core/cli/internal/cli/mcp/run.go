package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/manifests"
)

// RunCfg contains configuration for MCP run command
type RunCfg struct {
	ProjectDir  string
	NoInspector bool
	Transport   string
}

func RunMcp(cfg *RunCfg) error {
	projectDir, err := getProjectDir(cfg)
	if err != nil {
		return err
	}

	manifest, err := getProjectManifest(projectDir)
	if err != nil {
		return err
	}

	// Check if npx is installed (only needed when using inspector)
	if !cfg.NoInspector {
		if err := checkNpxInstalled(); err != nil {
			return err
		}
	}

	// Determine framework and create configuration
	switch manifest.Framework {
	case "fastmcp-python":
		return runFastMCPPython(cfg, projectDir, manifest)
	case "mcp-go":
		return runMCPGo(cfg, projectDir, manifest)
	case "typescript":
		return runTypeScript(cfg, projectDir, manifest)
	case "java":
		return runJava(cfg, projectDir, manifest)
	default:
		return fmt.Errorf("unsupported framework: %s", manifest.Framework)
	}
}

func runFastMCPPython(cfg *RunCfg, projectDir string, manifest *manifests.ProjectManifest) error {
	// Check if uv is available
	if _, err := exec.LookPath("uv"); err != nil {
		uvInstallURL := "https://docs.astral.sh/uv/getting-started/installation/"
		return fmt.Errorf(
			"uv is required for this command to run fastmcp-python projects locally. Please install uv: %s", uvInstallURL,
		)
	}

	appCfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	// Run uv sync first
	if appCfg.Verbose {
		fmt.Printf("Running uv sync in: %s\n", projectDir)
	}
	syncCmd := exec.Command("uv", "sync")
	syncCmd.Dir = projectDir
	syncCmd.Stdout = os.Stdout
	syncCmd.Stderr = os.Stderr
	if err := syncCmd.Run(); err != nil {
		return fmt.Errorf("failed to run uv sync: %w", err)
	}

	if cfg.NoInspector {
		// Run the server directly
		fmt.Printf("Running server directly: uv run python src/main.py\n")
		fmt.Printf("Server is running and waiting for MCP protocol input on stdin...\n")
		fmt.Printf("Press Ctrl+C to stop the server\n")

		serverCmd := exec.Command("uv", "run", "python", "src/main.py")
		serverCmd.Dir = projectDir
		serverCmd.Stdout = os.Stdout
		serverCmd.Stderr = os.Stderr
		serverCmd.Stdin = os.Stdin
		return serverCmd.Run()
	}

	// Create server configuration for inspector
	serverConfig := map[string]any{
		"command": "uv",
		"args":    []string{"run", "python", "src/main.py"},
	}

	// Create MCP inspector config
	configPath := filepath.Join(projectDir, "mcp-server-config.json")
	if err := createMCPInspectorConfig(manifest.Name, serverConfig, configPath); err != nil {
		return err
	}

	// Run the inspector
	return runMCPInspector(configPath, manifest.Name, projectDir)
}

func runMCPGo(cfg *RunCfg, projectDir string, manifest *manifests.ProjectManifest) error {
	// Check if go is available
	if _, err := exec.LookPath("go"); err != nil {
		goInstallURL := "https://golang.org/doc/install"
		return fmt.Errorf("go is required to run mcp-go projects locally. Please install Go: %s", goInstallURL)
	}

	appCfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	// Run go mod tidy first to ensure dependencies are up to date
	if appCfg.Verbose {
		fmt.Printf("Running go mod tidy in: %s\n", projectDir)
	}
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = projectDir
	tidyCmd.Stdout = os.Stdout
	tidyCmd.Stderr = os.Stderr
	if err := tidyCmd.Run(); err != nil {
		return fmt.Errorf("failed to run go mod tidy: %w", err)
	}

	if cfg.NoInspector {
		// Run the server directly
		fmt.Printf("Running server directly: go run main.go\n")
		fmt.Printf("Server is running and waiting for MCP protocol input on stdin...\n")
		fmt.Printf("Press Ctrl+C to stop the server\n")

		serverCmd := exec.Command("go", "run", "main.go")
		serverCmd.Dir = projectDir
		serverCmd.Stdout = os.Stdout
		serverCmd.Stderr = os.Stderr
		serverCmd.Stdin = os.Stdin
		return serverCmd.Run()
	}

	// Create server configuration for inspector
	serverConfig := map[string]any{
		"command": "go",
		"args":    []string{"run", "cmd/server/main.go"},
	}

	// Create MCP inspector config
	configPath := filepath.Join(projectDir, "mcp-server-config.json")
	if err := createMCPInspectorConfig(manifest.Name, serverConfig, configPath); err != nil {
		return err
	}

	// Run the inspector
	return runMCPInspector(configPath, manifest.Name, projectDir)
}

func getProjectDir(cfg *RunCfg) (string, error) {
	appCfg, err := config.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get config: %w", err)
	}
	// Determine project directory
	dir := cfg.ProjectDir
	if dir == "" {
		// Use current working directory
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
	} else {
		// Convert relative path to absolute path
		if !filepath.IsAbs(dir) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("failed to get current directory: %w", err)
			}
			dir = filepath.Join(cwd, dir)
		}
	}

	if appCfg.Verbose {
		fmt.Printf("Using project directory: %s\n", dir)
	}

	return dir, nil
}

func getProjectManifest(projectDir string) (*manifests.ProjectManifest, error) {
	// Check if manifest.yaml exists
	manager := manifests.NewManager(projectDir)
	if !manager.Exists() {
		return nil, fmt.Errorf("this directory is not an mcp-server directory: manifest.yaml not found")
	}

	// Load the manifest
	manifest, err := manager.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load manifest.yaml: %w", err)
	}

	return manifest, nil
}

func runTypeScript(cfg *RunCfg, projectDir string, manifest *manifests.ProjectManifest) error {
	// Check if npm is available
	if _, err := exec.LookPath("npm"); err != nil {
		npmInstallURL := "https://docs.npmjs.com/downloading-and-installing-node-js-and-npm"
		return fmt.Errorf("npm is required to run TypeScript projects locally. Please install Node.js and npm: %s", npmInstallURL)
	}

	appCfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	// Install dependencies first
	if appCfg.Verbose {
		fmt.Printf("Installing dependencies in: %s\n", projectDir)
	}
	installCmd := exec.Command("npm", "install")
	installCmd.Dir = projectDir
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install dependencies: %w", err)
	}

	if cfg.NoInspector {
		// Run the server directly with tsx (like Python uses uv run python)
		fmt.Printf("Running server directly: npx tsx src/index.ts\n")
		fmt.Printf("Server is running and waiting for MCP protocol input on stdin...\n")
		fmt.Printf("Press Ctrl+C to stop the server\n")

		serverCmd := exec.Command("npx", "tsx", "src/index.ts")
		serverCmd.Dir = projectDir
		serverCmd.Stdout = os.Stdout
		serverCmd.Stderr = os.Stderr
		serverCmd.Stdin = os.Stdin
		return serverCmd.Run()
	}

	// Create server configuration for inspector
	serverConfig := map[string]any{
		"command": "npx",
		"args":    []string{"tsx", "src/index.ts"},
	}

	// Create MCP inspector config
	configPath := filepath.Join(projectDir, "mcp-server-config.json")
	if err := createMCPInspectorConfig(manifest.Name, serverConfig, configPath); err != nil {
		return err
	}

	// Run the inspector
	return runMCPInspector(configPath, manifest.Name, projectDir)
}

func runJava(cfg *RunCfg, projectDir string, manifest *manifests.ProjectManifest) error {
	// Check if mvn is available
	if _, err := exec.LookPath("mvn"); err != nil {
		mvnInstallURL := "https://maven.apache.org/install.html"
		return fmt.Errorf("mvn is required to run Java projects locally. Please install Maven: %s", mvnInstallURL)
	}

	appCfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	// Run mvn clean install first to ensure dependencies are up to date
	if appCfg.Verbose {
		fmt.Printf("Running mvn clean install in: %s\n", projectDir)
	}
	installCmd := exec.Command("mvn", "clean", "install", "-DskipTests")
	installCmd.Dir = projectDir
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to run mvn clean install: %w", err)
	}

	// Prepare Maven arguments based on transport mode
	mavenArgs := []string{"exec:java", "-q", "-Dexec.mainClass=com.example.Main"}
	if cfg.Transport == transportHTTP {
		mavenArgs = append(mavenArgs, "-Dexec.args=--transport http --host 0.0.0.0 --port 3000")
	}

	if cfg.NoInspector {
		// Run the server directly
		if cfg.Transport == transportHTTP {
			fmt.Printf("Running server directly: mvn exec:java -Dexec.mainClass=\"com.example.Main\" --transport http --host 0.0.0.0 --port 3000\n")
			fmt.Printf("Server is running on http://localhost:3000\n")
			fmt.Printf("Health check: http://localhost:3000/health\n")
			fmt.Printf("MCP endpoint: http://localhost:3000/mcp\n")
		} else {
			fmt.Printf("Running server directly: mvn exec:java -Dexec.mainClass=\"com.example.Main\"\n")
			fmt.Printf("Server is running and waiting for MCP protocol input on stdin...\n")
		}
		fmt.Printf("Press Ctrl+C to stop the server\n")

		serverCmd := exec.Command("mvn", mavenArgs...)
		serverCmd.Dir = projectDir
		serverCmd.Stdout = os.Stdout
		serverCmd.Stderr = os.Stderr
		serverCmd.Stdin = os.Stdin
		return serverCmd.Run()
	}

	// Create server configuration for inspector
	var serverConfig map[string]any
	if cfg.Transport == transportHTTP {
		serverConfig = map[string]any{
			"type": "streamable-http",
			"url":  "http://localhost:3000/mcp",
		}
	} else {
		serverConfig = map[string]any{
			"command": "mvn",
			"args":    mavenArgs,
		}
	}

	// Create MCP inspector config
	configPath := filepath.Join(projectDir, "mcp-server-config.json")
	if err := createMCPInspectorConfig(manifest.Name, serverConfig, configPath); err != nil {
		return err
	}

	// Run the inspector
	return runMCPInspector(configPath, manifest.Name, projectDir)
}
