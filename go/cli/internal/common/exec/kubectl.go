package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// KubectlExecutor provides utilities for running kubectl commands with consistent
// error handling, logging, and configuration.
type KubectlExecutor struct {
	// Verbose enables detailed logging of kubectl commands
	Verbose bool

	// Namespace sets the default namespace for kubectl commands
	Namespace string
}

// NewKubectlExecutor creates a new kubectl executor with the specified configuration.
func NewKubectlExecutor(verbose bool, namespace string) *KubectlExecutor {
	return &KubectlExecutor{
		Verbose:   verbose,
		Namespace: namespace,
	}
}

// CheckAvailability checks if kubectl is installed and accessible.
func (k *KubectlExecutor) CheckAvailability() error {
	cmd := exec.Command("kubectl", "version", "--client")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl not found or not working: %w", err)
	}
	return nil
}

// Run executes a kubectl command with the given arguments.
// It automatically handles verbose logging and captures stderr for better error messages.
func (k *KubectlExecutor) Run(args ...string) error {
	if k.Verbose {
		fmt.Printf("Running: kubectl %s\n", strings.Join(args, " "))
	}

	cmd := exec.Command("kubectl", args...)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`kubectl %s` failed: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}

	return nil
}

// RunWithOutput executes a kubectl command and returns the combined output.
// This is useful for capturing command output for further processing.
func (k *KubectlExecutor) RunWithOutput(args ...string) ([]byte, error) {
	if k.Verbose {
		fmt.Printf("Running: kubectl %s\n", strings.Join(args, " "))
	}

	cmd := exec.Command("kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("`kubectl %s` failed: %w", strings.Join(args, " "), err)
	}

	return output, nil
}

// Apply applies YAML resources to the Kubernetes cluster.
// Multiple YAML documents can be provided and will be combined with separators.
func (k *KubectlExecutor) Apply(yamls ...[]byte) error {
	fmt.Printf("🚀 Applying resources to cluster...\n")

	// Check if kubectl is available
	if err := k.CheckAvailability(); err != nil {
		return fmt.Errorf("kubectl is required for cluster deployment: %w", err)
	}

	// Create temporary file for kubectl apply
	tmpFile, err := os.CreateTemp("", "resources-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			fmt.Printf("Warning: failed to remove temp file: %v\n", err)
		}
	}()

	// Combine all YAML resources with separators
	var combinedYAML []byte
	for i, yaml := range yamls {
		if i > 0 {
			combinedYAML = append(combinedYAML, []byte("\n---\n")...)
		}
		combinedYAML = append(combinedYAML, yaml...)
	}

	if _, err := tmpFile.Write(combinedYAML); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Apply using kubectl
	if err := k.Run("apply", "-f", tmpFile.Name()); err != nil {
		return err
	}

	fmt.Printf("✅ Resources applied successfully\n")
	return nil
}

// RolloutRestart restarts a Kubernetes deployment by triggering a rollout restart.
// This causes the deployment to recreate all pods with the latest configuration.
func (k *KubectlExecutor) RolloutRestart(name string) error {
	namespace := k.Namespace
	if namespace == "" {
		namespace = "default"
	}

	args := []string{
		"rollout", "restart", "deployment", name,
		"-n", namespace,
	}

	if k.Verbose {
		fmt.Printf("Restarting deployment '%s' in namespace '%s'...\n", name, namespace)
	}

	if err := k.Run(args...); err != nil {
		return fmt.Errorf("failed to restart deployment: %w", err)
	}

	return nil
}

// WaitForDeployment waits for a Kubernetes deployment to be ready.
// It uses kubectl rollout status to monitor the deployment progress.
func (k *KubectlExecutor) WaitForDeployment(name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	namespace := k.Namespace
	if namespace == "" {
		namespace = "default"
	}

	args := []string{
		"rollout", "status", "deployment", name,
		"-n", namespace,
		"--timeout", timeout.String(),
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if k.Verbose {
		fmt.Printf("Running: kubectl %s\n", strings.Join(args, " "))
	}

	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	// Sleep briefly to allow controller to create the deployment
	time.Sleep(1 * time.Second)

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timed out waiting for deployment to be ready")
		}
		return fmt.Errorf("`kubectl rollout status` failed: %w\n%s", err, stderr.String())
	}

	return nil
}
