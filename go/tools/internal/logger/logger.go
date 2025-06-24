package logger

import (
	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"
)

var globalLogger logr.Logger

// Init initializes the global logger with appropriate configuration
func Init() {
	// Set log level from environment variable (not directly supported by stdr, but can be extended)
	// For now, just use stdr with default settings
	globalLogger = stdr.New(nil)
}

// Get returns the global logger instance
func Get() logr.Logger {
	if globalLogger.GetSink() == nil {
		Init()
	}
	return globalLogger
}

// LogExecCommand logs information about an exec command being executed
func LogExecCommand(command string, args []string, caller string) {
	logger := Get()
	logger.Info("executing command",
		"command", command,
		"args", args,
		"caller", caller,
	)
}

// LogExecCommandResult logs the result of an exec command
func LogExecCommandResult(command string, args []string, output string, err error, duration float64, caller string) {
	logger := Get()

	if err != nil {
		logger.Error(err, "command execution failed",
			"command", command,
			"args", args,
			"duration_seconds", duration,
			"caller", caller,
		)
	} else {
		logger.Info("command execution successful",
			"command", command,
			"args", args,
			"output", output,
			"duration_seconds", duration,
			"caller", caller,
		)
	}
}

// Sync is a no-op for logr/stdr
func Sync() {}
