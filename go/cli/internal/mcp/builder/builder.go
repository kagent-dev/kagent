package builder

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options contains configuration for building MCP servers
type Options struct {
	ProjectDir string
	Tag        string
	Platform   string
	Verbose    bool
}

// Builder handles building MCP servers
type Builder struct {
	// Future: Add fields for template handling, etc.
}

// New creates a new Builder instance
func New() *Builder {
	return &Builder{}
}

// Build executes the build process for an MCP server
func (b *Builder) Build(opts Options) error {
	if opts.Verbose {
		fmt.Printf("Starting build process...\n")
	}

	// Detect project type
	projectType, err := b.detectProjectType(opts.ProjectDir)
	if err != nil {
		return fmt.Errorf("failed to detect project type: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Detected project type: %s\n", projectType)
	}

	// Build based on project type
	switch projectType {
	case "python":
		return b.buildDockerImage(opts, "python")
	case "node":
		return b.buildDockerImage(opts, "node")
	case "go":
		return b.buildDockerImage(opts, "go")
	case "java":
		return b.buildDockerImage(opts, "java")
	default:
		return fmt.Errorf("unsupported project type: %s", projectType)
	}
}

// detectProjectType determines the project type based on files present
func (b *Builder) detectProjectType(dir string) (string, error) {
	// Check for Python project
	if b.fileExists(filepath.Join(dir, "pyproject.toml")) ||
		b.fileExists(filepath.Join(dir, ".python-version")) ||
		b.fileExists(filepath.Join(dir, "requirements.txt")) ||
		b.fileExists(filepath.Join(dir, "setup.py")) {
		return "python", nil
	}

	// Check for Node.js project
	if b.fileExists(filepath.Join(dir, "package.json")) {
		return "node", nil
	}

	// Check for Go project
	if b.fileExists(filepath.Join(dir, "go.mod")) {
		return "go", nil
	}

	// Check for Java project
	if b.fileExists(filepath.Join(dir, "pom.xml")) {
		return "java", nil
	}

	return "", fmt.Errorf("unknown project type")
}

// fileExists checks if a file exists
func (b *Builder) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// buildDockerImage builds a Docker image for the MCP server
func (b *Builder) buildDockerImage(opts Options, projectType string) error {
	fmt.Printf("Building Docker image for %s project...\n", projectType)

	// Check if Docker is available
	if err := b.checkDockerAvailable(); err != nil {
		return fmt.Errorf("docker not available: %w", err)
	}

	// Check if Dockerfile exists
	dockerfilePath := filepath.Join(opts.ProjectDir, "Dockerfile")
	if !b.fileExists(dockerfilePath) {
		return fmt.Errorf("dockerfile not found at %s", dockerfilePath)
	}

	// Generate image name if not provided
	imageName := opts.Tag

	// Prepare docker build command
	args := []string{"build", "-t", imageName}

	// Add platform if specified
	if opts.Platform != "" {
		args = append(args, "--platform", opts.Platform)
	}

	// Add context (current directory)
	args = append(args, ".")

	if opts.Verbose {
		fmt.Printf("Running: docker %s\n", strings.Join(args, " "))
	}

	// Create docker command
	cmd := exec.Command("docker", args...)
	cmd.Dir = opts.ProjectDir

	// Show real-time output from docker build
	return b.runCommandWithOutput(cmd, imageName)
}

// checkDockerAvailable verifies that Docker is available and running
func (b *Builder) checkDockerAvailable() error {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not available or not running. Please ensure Docker is installed and running")
	}
	return nil
}

// runCommandWithOutput runs a command and streams output in real-time
func (b *Builder) runCommandWithOutput(cmd *exec.Cmd, imageName string) error {
	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start docker build: %w", err)
	}

	// Stream output
	go b.streamOutput(stdout, "")
	go b.streamOutput(stderr, "")

	// Wait for command to complete
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	fmt.Printf("✓ Successfully built Docker image: %s\n", imageName)
	return nil
}

// streamOutput reads from a pipe and outputs lines with optional prefix
func (b *Builder) streamOutput(pipe io.ReadCloser, _ string) {
	defer func() {
		if err := pipe.Close(); err != nil {
			fmt.Printf("Error closing pipe: %v\n", err)
		}
	}()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
	}
}
