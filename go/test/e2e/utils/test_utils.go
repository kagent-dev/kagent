package utils

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	kagentclient "github.com/kagent-dev/kagent/go/pkg/client"
)

func RunKubectl(ctx context.Context, stdin string, args ...string) error {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExtractClientError extracts a ClientError from an error chain, if present
func ExtractClientError(err error) (*kagentclient.ClientError, bool) {
	var clientErr *kagentclient.ClientError
	if errors.As(err, &clientErr) {
		return clientErr, true
	}
	return nil, false
}

// Poll checks a condition until it returns true or the context is done
func Poll(ctx context.Context, description string, checkFn func() bool, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s", description)
		case <-ticker.C:
			if checkFn() {
				return nil
			}
		}
	}
}

// checkPortForwardWorking verifies that port-forward is actually working by making an HTTP request
func checkPortForwardWorking() bool {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("http://localhost:8083/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// EnsurePortForward ensures that port-forward to kagent-controller is running on localhost:8083
// This is needed because when the controller restarts, any existing port-forward gets cancelled
// Returns a cleanup function that kills the port-forward process
func EnsurePortForward() (func(), error) {
	// Check if we're using localhost:8083 (which requires port-forward)
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL != "" && !strings.Contains(kagentURL, "localhost:8083") {
		// Not using localhost:8083, so port-forward not needed
		return func() {}, nil
	}

	// Check if port-forward is already working
	if checkPortForwardWorking() {
		return func() {}, nil
	}

	// Port-forward is not working, start it in the background
	cmd := exec.Command("kubectl", "port-forward", "-n", "kagent", "deployments/kagent-controller", "8083")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start port-forward in background
	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start port-forward: %w", err)
	}

	// Wait for port-forward to establish and verify it's working
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	portReady := false
	checkInterval := 500 * time.Millisecond
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for !portReady {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				cmd.Process.Kill()
				cmd.Process.Wait()
			}
			return nil, fmt.Errorf("timeout waiting for port-forward to be ready. Port 8083 may be in use or kubectl failed")
		case <-ticker.C:
			if checkPortForwardWorking() {
				portReady = true
			}
		}
	}

	// Return cleanup function to kill the port-forward process
	return func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Process.Wait()
		}
	}, nil
}
