package builder

import (
	"fmt"
	"os"
	"path/filepath"

	commonexec "github.com/kagent-dev/kagent/go/cli/internal/common/exec"
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

// buildDockerImage builds a Docker image
func (b *Builder) buildDockerImage(opts Options, projectType string) error {
	fmt.Printf("Building Docker image for %s project...\n", projectType)

	docker := commonexec.NewDockerExecutor(opts.Verbose, opts.ProjectDir)
	if err := docker.CheckAvailability(); err != nil {
		return fmt.Errorf("docker not available: %w", err)
	}

	dockerfilePath := filepath.Join(opts.ProjectDir, "Dockerfile")
	if !b.fileExists(dockerfilePath) {
		return fmt.Errorf("dockerfile not found at %s", dockerfilePath)
	}

	imageName := opts.Tag
	var extraArgs []string
	if opts.Platform != "" {
		extraArgs = append(extraArgs, "--platform", opts.Platform)
	}

	return docker.Build(imageName, ".", extraArgs...)
}
