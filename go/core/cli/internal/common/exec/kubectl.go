package exec

import (
	"fmt"
	"os/exec"
	"strings"
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
