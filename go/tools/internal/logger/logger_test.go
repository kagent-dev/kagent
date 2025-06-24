package logger

import (
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	// Test initialization
	Init()
	assert.NotNil(t, globalLogger)
}

func TestGet(t *testing.T) {
	// Reset global logger
	globalLogger = logr.Logger{}

	// Test Get without Init
	logger := Get()
	assert.NotNil(t, logger)
	assert.NotNil(t, globalLogger)
}

func TestLogExecCommand(t *testing.T) {
	// Just test that it does not panic and logs
	assert.NotPanics(t, func() {
		LogExecCommand("test-command", []string{"arg1", "arg2"}, "test.go:123")
	})
}

func TestLogExecCommandResult(t *testing.T) {
	// Test successful command
	assert.NotPanics(t, func() {
		LogExecCommandResult("test-command", []string{"arg1"}, "success output", nil, 1.5, "test.go:123")
	})
	// Test failed command
	assert.NotPanics(t, func() {
		LogExecCommandResult("test-command", []string{"arg1"}, "error output", assert.AnError, 0.5, "test.go:123")
	})
}

func TestEnvironmentVariables(t *testing.T) {
	// Test log level from environment (no-op for stdr)
	os.Setenv("KAGENT_LOG_LEVEL", "debug")
	defer os.Unsetenv("KAGENT_LOG_LEVEL")

	// Reset global logger
	globalLogger = logr.Logger{}

	// Initialize with environment variable
	Init()

	// Just check logger is set
	assert.NotNil(t, globalLogger)
}

func TestDevelopmentMode(t *testing.T) {
	// Test development mode (no-op for stdr)
	os.Setenv("KAGENT_ENV", "development")
	defer os.Unsetenv("KAGENT_ENV")

	// Reset global logger
	globalLogger = logr.Logger{}

	// Initialize in development mode
	Init()

	// In development mode, the logger should be configured (no panic)
	assert.NotNil(t, globalLogger)
}

func TestSync(t *testing.T) {
	// Test Sync function
	Init()

	// Sync should not panic
	assert.NotPanics(t, func() {
		Sync()
	})
}
