package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	kagentmemory "github.com/kagent-dev/kagent/go/adk/pkg/memory"
	runnerpkg "github.com/kagent-dev/kagent/go/adk/pkg/runner"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

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

	configDir := *filepathFlag
	if configDir == "" {
		configDir = os.Getenv("CONFIG_DIR")
	}
	if configDir == "" {
		configDir = "/config"
	}

	kagentURL := os.Getenv("KAGENT_URL")

	agentConfig, agentCard, err := config.LoadAgentConfigs(configDir)
	if err != nil {
		logger.Error(err, "Failed to load agent config (model configuration is required)", "configDir", configDir)
		os.Exit(1)
	}
	logger.Info("Loaded agent config", "configDir", configDir)
	logger.Info("Agent configuration",
		"model", agentConfig.Model.GetType(),
		"stream", agentConfig.GetStream(),
		"httpTools", len(agentConfig.HttpTools),
		"sseTools", len(agentConfig.SseTools),
		"remoteAgents", len(agentConfig.RemoteAgents))

	kagentName := os.Getenv("KAGENT_NAME")
	kagentNamespace := os.Getenv("KAGENT_NAMESPACE")

	// Derive app name from env or agent card.
	appName := deriveAppName(kagentName, kagentNamespace, agentCard, logger)

	// Fall back to appName / "default" so traces always have a non-empty service identity.
	serviceNameSource := kagentName
	if serviceNameSource == "" {
		serviceNameSource = appName
	}
	serviceNamespaceSource := kagentNamespace
	if serviceNamespaceSource == "" {
		serviceNamespaceSource = "default"
	}
	serviceName := strings.ReplaceAll(serviceNameSource, "-", "_")
	serviceNamespace := strings.ReplaceAll(serviceNamespaceSource, "-", "_")
	shutdownTelemetry, telemetryEnabled, telErr := telemetry.Init(context.Background(), serviceName, serviceNamespace)
	if telErr != nil {
		logger.Error(telErr, "Failed to initialize ADK telemetry providers; continuing without telemetry export")
	} else if telemetryEnabled {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := shutdownTelemetry(shutdownCtx); err != nil {
				logger.Error(err, "Failed to shutdown telemetry providers cleanly")
			}
		}()
		logger.Info("ADK telemetry initialized")
	} else {
		logger.Info("ADK telemetry disabled (set OTEL_TRACING_ENABLED or OTEL_LOGGING_ENABLED to true)")
	}

	// Create authenticated HTTP client when kagent persistence is enabled.
	// This client is shared between the executor's session service and
	// app.New's task store, avoiding duplicate token services.
	var httpClient *http.Client
	var tokenService *auth.KAgentTokenService
	if kagentURL != "" {
		tokenService = auth.NewKAgentTokenService(appName)
		if err := tokenService.Start(context.Background()); err != nil {
			logger.Error(err, "Failed to start token service")
		} else {
			logger.Info("Token service started")
		}
		defer tokenService.Stop()
		httpClient = auth.NewHTTPClientWithToken(tokenService)
	}

	// The executor needs a session service for its BeforeExecute callback
	// (session creation/lookup). This must be created before the executor.
	var sessionService *session.KAgentSessionService
	if kagentURL != "" {
		sessionService = session.NewKAgentSessionService(kagentURL, httpClient)
		logger.Info("Using KAgent session service", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, using in-memory session and no task persistence")
	}

	ctx := logr.NewContext(context.Background(), logger)

	// Build memory service if configured.
	var memoryService *kagentmemory.KagentMemoryService
	if agentConfig.Memory != nil && kagentURL != "" {
		memSvc, err := kagentmemory.New(kagentmemory.Config{
			AgentName:       appName,
			APIURL:          kagentURL,
			HTTPClient:      httpClient,
			TTLDays:         agentConfig.Memory.TTLDays,
			EmbeddingConfig: agentConfig.Memory.Embedding,
		})
		if err != nil {
			logger.Error(err, "Failed to create memory service")
			os.Exit(1)
		}
		memoryService = memSvc
		logger.Info("Memory service enabled", "appName", appName)
	}

	runnerConfig, subagentSessionIDs, err := runnerpkg.CreateRunnerConfig(ctx, agentConfig, sessionService, appName, memoryService)
	if err != nil {
		logger.Error(err, "Failed to create Google ADK Runner config")
		os.Exit(1)
	}

	stream := agentConfig.GetStream()
	executor := a2a.NewKAgentExecutor(a2a.KAgentExecutorConfig{
		RunnerConfig:       runnerConfig,
		SubagentSessionIDs: subagentSessionIDs,
		SessionService:     sessionService,
		Stream:             stream,
		AppName:            appName,
		Logger:             logger,
		RetryPolicy:        agentConfig.RetryPolicy,
	})

	// Build the agent card.
	if agentCard == nil {
		agentCard = &a2atype.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
			Version:     "0.2.0",
		}
	}
	agentCard.Capabilities = a2atype.AgentCapabilities{
		Streaming:              stream,
		StateTransitionHistory: true,
	}

	// Delegate server, task store, and remaining infrastructure to app.New.
	// Passing HTTPClient prevents app.New from creating a second token service.
	kagentApp, err := app.New(app.AppConfig{
		AgentCard:       *agentCard,
		Host:            *host,
		Port:            port,
		KAgentURL:       kagentURL,
		AppName:         appName,
		ShutdownTimeout: 5 * time.Second,
		Logger:          logger,
		HTTPClient:      httpClient,
		Agent:           runnerConfig.Agent,
	}, executor)
	if err != nil {
		logger.Error(err, "Failed to create app")
		os.Exit(1)
	}

	if err := kagentApp.Run(); err != nil {
		logger.Error(err, "Server error")
		os.Exit(1)
	}
}

func deriveAppName(kagentName, kagentNamespace string, agentCard *a2atype.AgentCard, logger logr.Logger) string {
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
		logger.Info("Using agent card name as app_name", "app_name", agentCard.Name)
		return agentCard.Name
	}

	logger.Info("Using default app_name", "app_name", "go-adk-agent")
	return "go-adk-agent"
}
