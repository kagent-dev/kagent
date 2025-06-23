package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var globalLogger *zap.Logger

// Init initializes the global logger with appropriate configuration
func Init() {
	config := zap.NewProductionConfig()

	// Set log level from environment variable
	logLevel := os.Getenv("KAGENT_LOG_LEVEL")
	if logLevel != "" {
		var level zapcore.Level
		if err := level.UnmarshalText([]byte(logLevel)); err == nil {
			config.Level = zap.NewAtomicLevelAt(level)
		}
	}

	// Configure for better readability in development
	if os.Getenv("KAGENT_ENV") == "development" {
		config.Development = true
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// Add caller information for better debugging
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var err error
	globalLogger, err = config.Build()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
}

// Get returns the global logger instance
func Get() *zap.Logger {
	if globalLogger == nil {
		Init()
	}
	return globalLogger
}

// LogExecCommand logs information about an exec command being executed
func LogExecCommand(command string, args []string, caller string) {
	logger := Get()
	logger.Info("!!",
		zap.String("command", command),
		zap.Strings("args", args),
		zap.String("caller", caller),
	)
}

// LogExecCommandResult logs the result of an exec command
func LogExecCommandResult(command string, args []string, output string, err error, duration float64, caller string) {
	logger := Get()

	if err != nil {
		logger.Error("KO",
			zap.String("command", command),
			zap.Strings("args", args),
			zap.String("error", err.Error()),
			zap.Float64("duration_seconds", duration),
			zap.String("caller", caller),
		)
	} else {
		logger.Info("OK",
			zap.String("command", command),
			zap.Strings("args", args),
			zap.String("output", output),
			zap.Float64("duration_seconds", duration),
			zap.String("caller", caller),
		)
	}
}

// Sync flushes any buffered log entries
func Sync() {
	if globalLogger != nil {
		globalLogger.Sync()
	}
}
