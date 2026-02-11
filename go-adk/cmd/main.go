package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go-adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/auth"
	"github.com/kagent-dev/kagent/go-adk/pkg/config"
	"github.com/kagent-dev/kagent/go-adk/pkg/mcp"
	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	"github.com/kagent-dev/kagent/go-adk/pkg/runner"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/taskstore"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// buildAppName builds the app_name from KAGENT_NAMESPACE and KAGENT_NAME environment variables.
func buildAppName(ctx context.Context, agentCard *server.AgentCard) string {
	logger := logr.FromContextOrDiscard(ctx)
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

// setupLogger initializes and returns a logr.Logger with the specified log level.
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

	// Create a base context with the logger so all derived contexts propagate it.
	baseCtx := logr.NewContext(context.Background(), logger)

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
		agentConfig = &config.AgentConfig{
			Stream:      &streamDefault,
			ExecuteCode: &executeCodeDefault,
		}
		agentCard = &server.AgentCard{
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

	appName := buildAppName(baseCtx, agentCard)
	logger.Info("Final app_name for session creation", "app_name", appName)

	var tokenService *auth.KAgentTokenService
	if kagentURL != "" {
		tokenService = auth.NewKAgentTokenService(appName)
		if err := tokenService.Start(baseCtx); err != nil {
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
		sessionService = session.NewKAgentSessionService(kagentURL, httpClient)
		logger.Info("Using KAgent session service", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, using in-memory session (sessions will not persist)")
	}

	var taskStore *taskstore.KAgentTaskStore
	var pushNotificationStore *taskstore.KAgentPushNotificationStore
	if kagentURL != "" {
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

	skillsDirectory := os.Getenv("KAGENT_SKILLS_FOLDER")
	if skillsDirectory != "" {
		logger.Info("Skills directory configured", "directory", skillsDirectory)
	} else {
		skillsDirectory = "/skills"
		logger.Info("Using default skills directory", "directory", skillsDirectory)
	}

	initCtx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
	defer cancel()
	toolsets := mcp.CreateToolsets(initCtx, agentConfig.HttpTools, agentConfig.SseTools)
	adkRunner, err := runner.CreateGoogleADKRunner(initCtx, agentConfig, sessionService, toolsets, appName)
	if err != nil {
		logger.Error(err, "Failed to create Google ADK Runner")
		os.Exit(1)
	}

	stream := false
	if agentConfig != nil {
		stream = agentConfig.GetStream()
	}

	executor := a2a.NewA2aAgentExecutor(adkRunner, a2a.A2aAgentExecutorConfig{
		Stream:           stream,
		ExecutionTimeout: model.DefaultExecutionTimeout,
	}, sessionService, taskStore, appName)

	taskManager := a2a.NewADKTaskManager(executor, taskStore, pushNotificationStore)

	if agentCard == nil {
		agentCard = &server.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
			Version:     "0.1.0",
		}
	}

	a2aServer, err := server.NewA2AServer(*agentCard, taskManager)
	if err != nil {
		logger.Error(err, "Failed to create A2A server")
		os.Exit(1)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	mux.Handle("/", a2aServer.Handler())

	addr := ":" + port
	if *host != "" {
		addr = *host + ":" + port
	}
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logger.Info("Starting Go ADK server", "addr", addr, "host", *host, "port", port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err, "Server failed")
			os.Exit(1)
		}
	}()

	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(baseCtx, 5*time.Second)
	defer shutdownCancel()
	logger.Info("Shutting down server...")
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error(err, "Error shutting down server")
	}
}
