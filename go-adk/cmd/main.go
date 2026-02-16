package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/config"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/converter"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a/server"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/auth"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/taskstore"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
)

// buildAppName builds the app_name from KAGENT_NAMESPACE and KAGENT_NAME environment variables.
// Format: {namespace}__NS__{name} where dashes are replaced with underscores.
// This matches Python KAgentConfig.app_name = self.namespace + "__NS__" + self.name
// Falls back to agentCard.Name if environment variables are not set, or "go-adk-agent" as default.
func buildAppName(agentCard *a2aserver.AgentCard, logger logr.Logger) string {
	kagentName := os.Getenv("KAGENT_NAME")
	kagentNamespace := os.Getenv("KAGENT_NAMESPACE")

	// If both are set, use the Python format: namespace__NS__name
	if kagentNamespace != "" && kagentName != "" {
		// Replace dashes with underscores (matching Python: self._name.replace("-", "_"))
		namespace := strings.ReplaceAll(kagentNamespace, "-", "_")
		name := strings.ReplaceAll(kagentName, "-", "_")
		appName := namespace + "__NS__" + name
		logger.Info("Built app_name from environment variables",
			"KAGENT_NAMESPACE", kagentNamespace,
			"KAGENT_NAME", kagentName,
			"app_name", appName)
		return appName
	}

	// Fallback to agent card name if available
	if agentCard != nil && agentCard.Name != "" {
		logger.Info("Using agent card name as app_name (KAGENT_NAMESPACE/KAGENT_NAME not set)",
			"app_name", agentCard.Name)
		return agentCard.Name
	}

	// Default fallback
	logger.Info("Using default app_name (KAGENT_NAMESPACE/KAGENT_NAME not set and no agent card)",
		"app_name", "go-adk-agent")
	return "go-adk-agent"
}

// setupLogger initializes and returns a logr.Logger with the specified log level.
// The log level string is case-insensitive and supports: debug, info, warn/warning, error.
// Defaults to info level if an invalid level is provided.
// Returns both the logr.Logger and the underlying zap.Logger (for cleanup).
func setupLogger(logLevel string) (logr.Logger, *zap.Logger) {
	// Parse log level and set zap level
	var zapLevel zapcore.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn", "warning":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	// Configure zap logger with the specified level
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	zapLogger, err := config.Build()
	if err != nil {
		// Fallback to development logger if production config fails
		devConfig := zap.NewDevelopmentConfig()
		devConfig.Level = zap.NewAtomicLevelAt(zapLevel)
		zapLogger, _ = devConfig.Build()
	}
	logger := zapr.NewLogger(zapLogger)

	logger.Info("Logger initialized", "level", logLevel)
	return logger, zapLogger
}

func main() {
	// Parse command line flags
	logLevel := flag.String("log-level", "info", "Set the logging level (debug, info, warn, error)")
	host := flag.String("host", "", "Set the host address to bind to (default: empty, binds to all interfaces)")
	portFlag := flag.String("port", "", "Set the port to listen on (overrides PORT environment variable)")
	filepathFlag := flag.String("filepath", "", "Set the config directory path (overrides CONFIG_DIR environment variable)")
	flag.Parse()

	logger, zapLogger := setupLogger(*logLevel)
	defer func() {
		_ = zapLogger.Sync()
	}()

	// Get port from flag, environment variable, or default
	port := *portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	// Get config directory from flag, environment variable, or default
	configDir := *filepathFlag
	if configDir == "" {
		configDir = os.Getenv("CONFIG_DIR")
	}
	if configDir == "" {
		configDir = "/config"
	}

	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		kagentURL = "http://localhost:8083"
	}

	// Load agent configuration from config directory (matching Python implementation)
	agentConfig, agentCard, err := config.LoadAgentConfigs(configDir)
	if err != nil {
		logger.Info("Failed to load agent config, using default configuration", "configDir", configDir, "error", err)
		// Create default config if loading fails
		streamDefault := false
		executeCodeDefault := false
		agentConfig = &types.AgentConfig{
			Stream:      &streamDefault,
			ExecuteCode: &executeCodeDefault,
		}
		agentCard = &a2aserver.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
		}
	} else {
		logger.Info("Loaded agent config", "configDir", configDir)
		logger.Info("AgentConfig summary", "summary", config.GetAgentConfigSummary(agentConfig))
		logger.Info("Agent configuration",
			"model", agentConfig.Model.GetType(),
			"stream", agentConfig.GetStream(),
			"executeCode", agentConfig.GetExecuteCode(),
			"httpTools", len(agentConfig.HttpTools),
			"sseTools", len(agentConfig.SseTools),
			"remoteAgents", len(agentConfig.RemoteAgents))
	}

	// Build app_name from KAGENT_NAMESPACE and KAGENT_NAME (matching Python KAgentConfig.app_name)
	appName := buildAppName(agentCard, logger)
	logger.Info("Final app_name for session creation", "app_name", appName)

	// Create token service for k8s token management (matching Python implementation)
	var tokenService *auth.KAgentTokenService
	if kagentURL != "" {
		tokenService = auth.NewKAgentTokenService(appName)
		ctx := context.Background()
		if err := tokenService.Start(ctx); err != nil {
			logger.Error(err, "Failed to start token service")
		} else {
			logger.Info("Token service started")
		}
		defer tokenService.Stop()
	}

	// Create session service (use nil for in-memory if KAGENT_URL is not set)
	var sessionService session.SessionService
	if kagentURL != "" {
		// Use token service for authenticated requests
		var httpClient *http.Client
		if tokenService != nil {
			httpClient = auth.NewHTTPClientWithToken(tokenService)
		} else {
			httpClient = &http.Client{Timeout: 30 * time.Second}
		}
		sessionService = session.NewKAgentSessionServiceWithLogger(kagentURL, httpClient, logger)
		logger.Info("Using KAgent session service", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, using in-memory session (sessions will not persist)")
	}

	// Create task store for persisting tasks to KAgent
	var taskStore *taskstore.KAgentTaskStore
	var pushNotificationStore *taskstore.KAgentPushNotificationStore
	if kagentURL != "" {
		// Use token service for authenticated requests
		var httpClient *http.Client
		if tokenService != nil {
			httpClient = auth.NewHTTPClientWithToken(tokenService)
		} else {
			httpClient = &http.Client{Timeout: 30 * time.Second}
		}
		taskStore = taskstore.NewKAgentTaskStoreWithClient(kagentURL, httpClient)
		pushNotificationStore = taskstore.NewKAgentPushNotificationStoreWithClient(kagentURL, httpClient)
		logger.Info("Using KAgent task store", "url", kagentURL)
		logger.Info("Using KAgent push notification store", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, task persistence and push notifications disabled")
	}

	// Check for skills directory (matching Python's KAGENT_SKILLS_FOLDER)
	skillsDirectory := os.Getenv("KAGENT_SKILLS_FOLDER")
	if skillsDirectory != "" {
		logger.Info("Skills directory configured", "directory", skillsDirectory)
	} else {
		// Default to /skills if not set
		skillsDirectory = "/skills"
		logger.Info("Using default skills directory", "directory", skillsDirectory)
	}

	// Create runner (single ADK runner; no factory)
	agentRunner := adk.NewADKRunner(agentConfig, skillsDirectory, logger)

	// Use stream setting from agent config
	stream := false
	if agentConfig != nil {
		stream = agentConfig.GetStream()
	}

	executor := core.NewA2aAgentExecutorWithLogger(agentRunner, converter.NewEventConverter(), core.A2aAgentExecutorConfig{
		Stream:           stream,
		ExecutionTimeout: a2a.DefaultExecutionTimeout,
	}, sessionService, taskStore, appName, logger)

	taskManager := server.NewA2ATaskManager(executor, taskStore, pushNotificationStore, logger)

	// Use loaded agent card or create default
	if agentCard == nil {
		agentCard = &a2aserver.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
			Version:     "0.1.0",
		}
	}

	// Create and run A2A server
	serverConfig := server.ServerConfig{
		Host:            *host,
		Port:            port,
		ShutdownTimeout: 5 * time.Second,
	}

	a2aServer, err := server.NewA2AServer(*agentCard, taskManager, logger, serverConfig)
	if err != nil {
		logger.Error(err, "Failed to create A2A server")
		os.Exit(1)
	}

	if err := a2aServer.Run(); err != nil {
		logger.Error(err, "Server error")
		os.Exit(1)
	}
}
