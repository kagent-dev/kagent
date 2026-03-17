package testutil

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// RequireDocker skips the test if Docker is not available.
// Use this for integration tests that require Docker daemon.
func RequireDocker(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skipf("Docker daemon is not available, skipping test: %v", err)
	}
}

// RequireKind skips the test if kind is not available.
// Use this for integration tests that require kind cluster.
func RequireKind(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kind", "version")
	if err := cmd.Run(); err != nil {
		t.Skipf("kind is not available, skipping test: %v", err)
	}
}

// IsDockerAvailable returns true if Docker daemon is available.
func IsDockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "info")
	return cmd.Run() == nil
}

// IsKindAvailable returns true if kind is available.
func IsKindAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kind", "version")
	return cmd.Run() == nil
}
