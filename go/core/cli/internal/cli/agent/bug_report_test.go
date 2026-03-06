package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestBugReportCmd_DirectoryCreation(t *testing.T) {
	// Change to temp directory to avoid cluttering workspace
	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Capture stdout to check messages
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := &config.Config{
		Namespace: "test-namespace",
		Verbose:   false,
	}

	// Run bug report command
	// Note: This will fail on kubectl commands but should create the directory
	BugReportCmd(cfg)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output mentions bug report generation
	assert.Contains(t, output, "Bug report generated")
	assert.Contains(t, output, "kagent-bug-report-")

	// Verify a directory was created with the correct pattern
	entries, err := os.ReadDir(tmpDir)
	assert.NoError(t, err)

	foundBugReportDir := false
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "kagent-bug-report-") {
			foundBugReportDir = true
			break
		}
	}
	assert.True(t, foundBugReportDir, "Bug report directory should be created")
}

func TestBugReportCmd_DirectoryNaming(t *testing.T) {
	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Suppress output
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout, _ = os.Open(os.DevNull)
	os.Stderr, _ = os.Open(os.DevNull)
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	cfg := &config.Config{
		Namespace: "kagent",
	}

	BugReportCmd(cfg)

	// Check that directory follows naming pattern: kagent-bug-report-YYYYMMDD-HHMMSS
	entries, err := os.ReadDir(tmpDir)
	assert.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "kagent-bug-report-") {
			// Verify format
			parts := strings.Split(entry.Name(), "-")
			assert.GreaterOrEqual(t, len(parts), 3, "Directory name should have timestamp")
			return
		}
	}
	t.Error("No bug report directory found")
}

func TestBugReportCmd_WarningMessage(t *testing.T) {
	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Suppress stderr to avoid kubectl error messages
	oldStderr := os.Stderr
	os.Stderr, _ = os.Open(os.DevNull)
	defer func() { os.Stderr = oldStderr }()

	cfg := &config.Config{
		Namespace: "kagent",
	}

	BugReportCmd(cfg)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify warning about sensitive information is displayed
	assert.Contains(t, output, "WARNING")
	assert.Contains(t, output, "sensitive information")
	assert.Contains(t, output, "agent.yaml")
}

func TestBugReportCmd_ConfigNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{
			name:      "default namespace",
			namespace: "kagent",
		},
		{
			name:      "custom namespace",
			namespace: "my-custom-ns",
		},
		{
			name:      "empty namespace",
			namespace: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalWd, _ := os.Getwd()
			tmpDir := t.TempDir()
			os.Chdir(tmpDir)
			defer os.Chdir(originalWd)

			// Suppress all output
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			os.Stdout, _ = os.Open(os.DevNull)
			os.Stderr, _ = os.Open(os.DevNull)
			defer func() {
				os.Stdout = oldStdout
				os.Stderr = oldStderr
			}()

			cfg := &config.Config{
				Namespace: tt.namespace,
				Verbose:   false,
			}

			// Should not panic with any namespace
			BugReportCmd(cfg)

			// Verify directory was created
			entries, err := os.ReadDir(tmpDir)
			assert.NoError(t, err)
			assert.NotEmpty(t, entries, "Bug report directory should be created")
		})
	}
}

func TestBugReportCmd_VerboseMode(t *testing.T) {
	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Suppress output
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout, _ = os.Open(os.DevNull)
	os.Stderr, _ = os.Open(os.DevNull)
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	tests := []struct {
		name    string
		verbose bool
	}{
		{
			name:    "verbose enabled",
			verbose: true,
		},
		{
			name:    "verbose disabled",
			verbose: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Namespace: "kagent",
				Verbose:   tt.verbose,
			}

			// Should work with both verbose settings
			BugReportCmd(cfg)

			// Verify directory was created
			entries, err := os.ReadDir(tmpDir)
			assert.NoError(t, err)

			foundDir := false
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), "kagent-bug-report-") {
					foundDir = true
					// Clean up for next iteration
					os.RemoveAll(filepath.Join(tmpDir, entry.Name()))
					break
				}
			}
			assert.True(t, foundDir)
		})
	}
}
