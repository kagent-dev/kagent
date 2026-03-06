//go:build !darwin

package cli

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestDashboardCmd(t *testing.T) {
	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cfg := &config.Config{
		KAgentURL: "http://localhost:8080",
		Namespace: "kagent",
	}

	// Run the command
	DashboardCmd(context.Background(), cfg)

	// Restore stderr and read output
	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify expected messages are printed
	assert.Contains(t, output, "Dashboard is not available on this platform")
	assert.Contains(t, output, "kubectl port-forward")
	assert.Contains(t, output, "http://localhost:8082")
}

func TestDashboardCmd_OutputFormat(t *testing.T) {
	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cfg := &config.Config{}

	DashboardCmd(context.Background(), cfg)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify helpful instructions are provided
	assert.Contains(t, output, "service/kagent-ui")
	assert.Contains(t, output, "8082:8080")
}
