package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
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
)

func buildAppName(agentCard *a2atype.AgentCard, logger logr.Logger) string {
	kagentName := os.Getenv("KAGENT_NAME")
	kagentNamespace := os.Getenv("KAGENT_NAMESPACE")

	if kagentNamespace != "" && kagentName != "" {
		namespace := strings.ReplaceAll(kagentNamespace, "-", "_")
		name := strings.ReplaceAll(kagentName, "-", "_")
		appName := namespace + "__NS__" + name
		logger.Info("Built app_name from environment variables",
			"KAGENT_NAMESPACE", kagentNamespace,
			"KAGENT_NAME", kagentName,
			"app_name", appName)
		return appName
	}

	if agentCard != nil && agentCard.Name != "" {
		logger.Info("Using agent card name as app_name (KAGENT_NAMESPACE/KAGENT_NAME not set)",
			"app_name", agentCard.Name)
		return agentCard.Name
	}

	logger.Info("Using default app_name (KAGENT_NAMESPACE/KAGENT_NAME not set and no agent card)",
		"app_name", "go-adk-agent")
	return "go-adk-agent"
}

func setupLogger(logLevel string) (logr.Logger, *zap.Logger) {
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

	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(zapLevel)
	zapConfig.EncoderConfig.TimeKey = "timestamp"
	zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	zapLogger, err := zapConfig.Build()
	if err != nil {
		devConfig := zap.NewDevelopmentConfig()
		devConfig.Level = zap.NewAtomicLevelAt(zapLevel)
		zapLogger, _ = devConfig.Build()
	}
	logger := zapr.NewLogger(zapLogger)
	logger.Info("Logger initialized", "level", logLevel)
	return logger, zapLogger
}

func main() {
	logLevel := flag.String("log-level", "info", "Set the logging level (debug, info, warn, error)")
	host := flag.String("host", "", "Set the host address to bind to (default: empty, binds to all interfaces)")
	portFlag := flag.String("port", "", "Set the port to listen on (overrides PORT environment variable)")
	filepathFlag := flag.String("filepath", "", "Set the config directory path (overrides CONFIG_DIR environment variable)")
	flag.Parse()

	logger, zapLogger := setupLogger(*logLevel)
	defer func() {
		_ = zapLogger.Sync()
	}()

	port := *portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

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

	agentConfig, agentCard, err := config.LoadAgentConfigs(configDir)
	if err != nil {
		logger.Info("Failed to load agent config, using default configuration", "configDir", configDir, "error", err)
		streamDefault := false
		executeCodeDefault := false
		agentConfig = &types.AgentConfig{
			Stream:      &streamDefault,
			ExecuteCode: &executeCodeDefault,
		}
		agentCard = &a2atype.AgentCard{
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

	appName := buildAppName(agentCard, logger)
	logger.Info("Final app_name for session creation", "app_name", appName)

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

	var sessionService session.SessionService
	if kagentURL != "" {
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

	var taskStoreInstance *taskstore.KAgentTaskStore
	if kagentURL != "" {
		var httpClient *http.Client
		if tokenService != nil {
			httpClient = auth.NewHTTPClientWithToken(tokenService)
		} else {
			httpClient = &http.Client{Timeout: 30 * time.Second}
		}
		taskStoreInstance = taskstore.NewKAgentTaskStoreWithClient(kagentURL, httpClient)
		logger.Info("Using KAgent task store", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, task persistence disabled")
	}

	skillsDirectory := os.Getenv("KAGENT_SKILLS_FOLDER")
	if skillsDirectory != "" {
		logger.Info("Skills directory configured", "directory", skillsDirectory)
	} else {
		skillsDirectory = "/skills"
		logger.Info("Using default skills directory", "directory", skillsDirectory)
	}

	agentRunner := adk.NewADKRunner(agentConfig, skillsDirectory, logger)

	stream := false
	if agentConfig != nil {
		stream = agentConfig.GetStream()
	}

	executor := core.NewA2aAgentExecutorWithLogger(agentRunner, converter.NewEventConverter(), core.A2aAgentExecutorConfig{
		Stream:           stream,
		ExecutionTimeout: a2a.DefaultExecutionTimeout,
	}, sessionService, taskStoreInstance, appName, logger)

	// Create the a2asrv.AgentExecutor bridge
	agentExecutor := server.NewKAgentExecutor(executor)

	// Build handler options
	var handlerOpts []a2asrv.RequestHandlerOption
	if taskStoreInstance != nil {
		taskStoreAdapter := server.NewKAgentTaskStoreAdapter(taskStoreInstance)
		handlerOpts = append(handlerOpts, a2asrv.WithTaskStore(taskStoreAdapter))
	}

	if agentCard == nil {
		agentCard = &a2atype.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
			Version:     "0.2.0",
		}
	}

	serverConfig := server.ServerConfig{
		Host:            *host,
		Port:            port,
		ShutdownTimeout: 5 * time.Second,
	}

	a2aServer, err := server.NewA2AServer(*agentCard, agentExecutor, logger, serverConfig, handlerOpts...)
	if err != nil {
		logger.Error(err, "Failed to create A2A server")
		os.Exit(1)
	}

	if err := a2aServer.Run(); err != nil {
		logger.Error(err, "Server error")
		os.Exit(1)
	}
}
