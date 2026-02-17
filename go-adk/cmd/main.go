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
	"github.com/kagent-dev/kagent/go-adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/a2a/server"
	"github.com/kagent-dev/kagent/go-adk/pkg/auth"
	"github.com/kagent-dev/kagent/go-adk/pkg/config"
	"github.com/kagent-dev/kagent/go-adk/pkg/mcp"
	runnerpkg "github.com/kagent-dev/kagent/go-adk/pkg/runner"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/taskstore"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func defaultAgentCard() *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name:        "go-adk-agent",
		Description: "Go-based Agent Development Kit",
		Version:     "0.2.0",
	}
}

func newHTTPClient(tokenService *auth.KAgentTokenService) *http.Client {
	if tokenService != nil {
		return auth.NewHTTPClientWithToken(tokenService)
	}
	return &http.Client{Timeout: 30 * time.Second}
}

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

	// KAGENT_URL controls remote session/task persistence. When empty,
	// the agent falls back to in-memory sessions with no task persistence.
	kagentURL := os.Getenv("KAGENT_URL")

	agentConfig, agentCard, err := config.LoadAgentConfigs(configDir)
	if err != nil {
		logger.Error(err, "Failed to load agent config (model configuration is required)", "configDir", configDir)
		os.Exit(1)
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
	var taskStoreInstance *taskstore.KAgentTaskStore
	if kagentURL != "" {
		httpClient := newHTTPClient(tokenService)
		sessionService = session.NewKAgentSessionService(kagentURL, httpClient)
		logger.Info("Using KAgent session service", "url", kagentURL)
		taskStoreInstance = taskstore.NewKAgentTaskStoreWithClient(kagentURL, httpClient)
		logger.Info("Using KAgent task store", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, using in-memory session and no task persistence")
	}

	// Create MCP toolsets from configured HTTP and SSE servers
	ctx := logr.NewContext(context.Background(), logger)
	toolsets := mcp.CreateToolsets(ctx, agentConfig.HttpTools, agentConfig.SseTools)

	// Create Google ADK runner eagerly
	adkRunner, err := runnerpkg.CreateGoogleADKRunner(ctx, agentConfig, sessionService, toolsets, appName)
	if err != nil {
		logger.Error(err, "Failed to create Google ADK Runner")
		os.Exit(1)
	}

	stream := false
	if agentConfig != nil {
		stream = agentConfig.GetStream()
	}

	// Create executor that directly implements a2asrv.AgentExecutor
	executor := a2a.NewKAgentExecutor(adkRunner, sessionService, a2a.KAgentExecutorConfig{
		Stream:           stream,
		ExecutionTimeout: a2a.DefaultExecutionTimeout,
	}, appName)

	// Build handler options
	var handlerOpts []a2asrv.RequestHandlerOption
	if taskStoreInstance != nil {
		taskStoreAdapter := taskstore.NewA2ATaskStoreAdapter(taskStoreInstance)
		handlerOpts = append(handlerOpts, a2asrv.WithTaskStore(taskStoreAdapter))
	}


	if agentCard == nil {
		agentCard = defaultAgentCard()
	}

	serverConfig := server.ServerConfig{
		Host:            *host,
		Port:            port,
		ShutdownTimeout: 5 * time.Second,
	}

	a2aServer, err := server.NewA2AServer(*agentCard, executor, logger, serverConfig, handlerOpts...)
	if err != nil {
		logger.Error(err, "Failed to create A2A server")
		os.Exit(1)
	}

	if err := a2aServer.Run(); err != nil {
		logger.Error(err, "Server error")
		os.Exit(1)
	}
}
