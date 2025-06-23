package logger

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestInit(t *testing.T) {
	// Test initialization
	Init()
	assert.NotNil(t, globalLogger)
}

func TestGet(t *testing.T) {
	// Reset global logger
	globalLogger = nil

	// Test Get without Init
	logger := Get()
	assert.NotNil(t, logger)
	assert.NotNil(t, globalLogger)
}

func TestLogExecCommand(t *testing.T) {
	// Create a test logger with observer
	core, obs := observer.New(zap.InfoLevel)
	testLogger := zap.New(core)

	// Temporarily replace global logger
	originalLogger := globalLogger
	globalLogger = testLogger
	defer func() { globalLogger = originalLogger }()

	// Test logging exec command
	LogExecCommand("test-command", []string{"arg1", "arg2"}, "test.go:123")

	// Check that log entry was created
	assert.Equal(t, 1, obs.Len())

	logEntry := obs.All()[0]
	assert.Equal(t, "executing command", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)

	// Check fields
	assert.Equal(t, "test-command", logEntry.ContextMap()["command"])
	// Convert interface{} slice to string slice for comparison
	argsInterface := logEntry.ContextMap()["args"].([]interface{})
	args := make([]string, len(argsInterface))
	for i, v := range argsInterface {
		args[i] = v.(string)
	}
	assert.Equal(t, []string{"arg1", "arg2"}, args)
	assert.Equal(t, "test.go:123", logEntry.ContextMap()["caller"])
}

func TestLogExecCommandResult(t *testing.T) {
	// Create a test logger with observer
	core, obs := observer.New(zap.InfoLevel)
	testLogger := zap.New(core)

	// Temporarily replace global logger
	originalLogger := globalLogger
	globalLogger = testLogger
	defer func() { globalLogger = originalLogger }()

	// Test successful command
	LogExecCommandResult("test-command", []string{"arg1"}, "success output", nil, 1.5, "test.go:123")

	assert.Equal(t, 1, obs.Len())
	logEntry := obs.All()[0]
	assert.Equal(t, "command execution successful", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, "success output", logEntry.ContextMap()["output"])
	assert.Equal(t, float64(1.5), logEntry.ContextMap()["duration_seconds"])

	// Clear observer
	obs.TakeAll()

	// Test failed command
	LogExecCommandResult("test-command", []string{"arg1"}, "error output", assert.AnError, 0.5, "test.go:123")

	assert.Equal(t, 1, obs.Len())
	logEntry = obs.All()[0]
	assert.Equal(t, "command execution failed", logEntry.Message)
	assert.Equal(t, zap.ErrorLevel, logEntry.Level)
	assert.Equal(t, assert.AnError.Error(), logEntry.ContextMap()["error"])
}

func TestEnvironmentVariables(t *testing.T) {
	// Test log level from environment
	os.Setenv("KAGENT_LOG_LEVEL", "debug")
	defer os.Unsetenv("KAGENT_LOG_LEVEL")

	// Reset global logger
	globalLogger = nil

	// Initialize with environment variable
	Init()

	// Check that debug level is set
	assert.Equal(t, zap.DebugLevel, globalLogger.Level())
}

func TestDevelopmentMode(t *testing.T) {
	// Test development mode
	os.Setenv("KAGENT_ENV", "development")
	defer os.Unsetenv("KAGENT_ENV")

	// Reset global logger
	globalLogger = nil

	// Initialize in development mode
	Init()

	// In development mode, the logger should be configured differently
	// We can't easily test the exact configuration, but we can ensure it doesn't panic
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
