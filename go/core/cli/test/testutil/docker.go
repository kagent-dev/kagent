package testutil

import (
	"context"
	"os/exec"
	"testing"
)

// RequireDocker skips the test if Docker is not available.
// Use this for integration tests that require Docker daemon.
func RequireDocker(t *testing.T) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker daemon is not available, skipping test")
	}
}

// RequireKind skips the test if kind is not available.
// Use this for integration tests that require kind cluster.
func RequireKind(t *testing.T) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "kind", "version")
	if err := cmd.Run(); err != nil {
		t.Skip("kind is not available, skipping test")
	}
}

// IsDockerAvailable returns true if Docker daemon is available.
func IsDockerAvailable() bool {
	cmd := exec.CommandContext(context.Background(), "docker", "info")
	return cmd.Run() == nil
}

// IsKindAvailable returns true if kind is available.
func IsKindAvailable() bool {
	cmd := exec.CommandContext(context.Background(), "kind", "version")
	return cmd.Run() == nil
}
