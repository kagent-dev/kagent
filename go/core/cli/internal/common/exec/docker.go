// Package exec provides utilities for executing external commands.
package exec

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DockerExecutor provides utilities for running Docker commands with consistent
// error handling, logging, and configuration across the CLI.
type DockerExecutor struct {
	// Verbose enables detailed logging of Docker commands
	Verbose bool

	// WorkDir sets the working directory for Docker commands
	WorkDir string
}

// NewDockerExecutor creates a new Docker executor with the specified configuration.
func NewDockerExecutor(verbose bool, workDir string) *DockerExecutor {
	return &DockerExecutor{
		Verbose: verbose,
		WorkDir: workDir,
	}
}

// CheckAvailability checks if Docker is installed and the daemon is running.
// Returns an error if Docker is not available or not properly configured.
func (d *DockerExecutor) CheckAvailability() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker command not found in PATH. Please install Docker")
	}

	// Check if Docker daemon is running by querying the server version
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("docker daemon is not running or not accessible. Please start Docker Desktop or Docker daemon")
	}

	// Verify we got a valid version string
	version := strings.TrimSpace(string(output))
	if version == "" {
		return fmt.Errorf("docker daemon returned empty version. Docker may not be properly installed")
	}

	return nil
}

// Run executes a docker command with the given arguments.
// It automatically handles verbose logging and working directory configuration.
func (d *DockerExecutor) Run(args ...string) error {
	if d.Verbose {
		fmt.Printf("Running: docker %s\n", strings.Join(args, " "))
		if d.WorkDir != "" {
			fmt.Printf("Working directory: %s\n", d.WorkDir)
		}
	}

	cmd := exec.Command("docker", args...)
	if d.WorkDir != "" {
		cmd.Dir = d.WorkDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Build builds a Docker image with the specified name and context.
// Additional build arguments can be provided (e.g., "--platform", "linux/amd64").
func (d *DockerExecutor) Build(imageName, context string, extraArgs ...string) error {
	args := []string{"build", "-t", imageName}
	args = append(args, extraArgs...)
	args = append(args, context)

	if err := d.Run(args...); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	fmt.Printf("✅ Successfully built Docker image: %s\n", imageName)
	return nil
}

// Push pushes a Docker image to a registry.
func (d *DockerExecutor) Push(imageName string) error {
	if err := d.Run("push", imageName); err != nil {
		return fmt.Errorf("docker push failed: %w", err)
	}

	fmt.Printf("✅ Successfully pushed Docker image: %s\n", imageName)
	return nil
}

// GetComposeCommand returns the appropriate docker compose command for the system.
// It tries "docker compose" (newer version) first, then falls back to "docker-compose" (older version).
func GetComposeCommand() []string {
	// Try docker compose (newer version)
	if _, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command("docker", "compose", "version")
		if err := cmd.Run(); err == nil {
			return []string{"docker", "compose"}
		}
	}

	// Fall back to docker-compose (older version)
	return []string{"docker-compose"}
}
