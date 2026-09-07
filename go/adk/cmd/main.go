package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	kagentmemory "github.com/kagent-dev/kagent/go/adk/pkg/memory"
	runnerpkg "github.com/kagent-dev/kagent/go/adk/pkg/runner"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	defaultPluginPackagesRoot = "/plugins"
	defaultSkillsRoot         = "/skills"
	defaultPluginDataRoot     = "/data/plugins"
)

func main() {
	logLevel := flag.String("log-level", cmp.Or(os.Getenv("LOG_LEVEL"), "info"), "Set the logging level (debug, info, warn, error)")
	host := flag.String("host", "", "Set the host address to bind to (default: empty, binds to all interfaces)")
	portFlag := flag.String("port", "", "Set the port to listen on (overrides PORT environment variable)")
	filepathFlag := flag.String("filepath", "", "Set the config directory path (overrides CONFIG_DIR environment variable)")
	flag.Parse()

	logger, err := logging.New(os.Stderr, *logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level %q: %v\n", *logLevel, err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	logger.Info("logger initialized", "level", *logLevel)

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

	kagentAPIURL := env.KagentAPIURL.Get()

	if err := config.MaterializeFromEnv(configDir); err != nil {
		logger.Error("failed to materialize agent config from environment", "error", err, "config_dir", configDir)
		os.Exit(1)
	}

	agentConfig, agentCard, err := config.LoadAgentConfigs(configDir)
	if err != nil {
		logger.Error("failed to load agent config (model configuration is required)", "error", err, "config_dir", configDir)
		os.Exit(1)
	}
	if err := config.MaterializeAgentPlugins(
		logging.IntoContext(context.Background(), logger), agentConfig,
		config.AgentPluginPaths{
			Packages: defaultPluginPackagesRoot,
			Skills:   defaultSkillsRoot,
			Data:     defaultPluginDataRoot,
		},
	); err != nil {
		logger.Error("failed to materialize Agent Plugins", "error", err)
		os.Exit(1)
	}
	logger.Info("loaded agent config", "config_dir", configDir)
	logger.Info("agent configuration",
		"model", agentConfig.Model.GetType(),
		"stream", agentConfig.GetStream(),
		"http_tools", len(agentConfig.HttpTools),
		"sse_tools", len(agentConfig.SseTools),
		"remote_agents", len(agentConfig.RemoteAgents))

	kagentName := env.KagentName.Get()
	kagentNamespace := env.KagentNamespace.Get()

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
		logger.Error("failed to initialize ADK telemetry providers; continuing without telemetry export", "error", telErr)
	} else if telemetryEnabled {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := shutdownTelemetry(shutdownCtx); err != nil {
				logger.Error("failed to shutdown telemetry providers cleanly", "error", err)
			}
		}()
		logger.Info("telemetry initialized for ADK")
	} else {
		logger.Info("telemetry disabled for ADK (set OTEL_TRACING_ENABLED or OTEL_LOGGING_ENABLED to true)")
	}

	// Create one authenticated controller channel for all kagent persistence.
	var controllerClient *controllerclient.Client
	var tokenService *auth.KAgentTokenService
	if kagentAPIURL != "" {
		tokenService = auth.NewKAgentTokenService(appName)
		if err := tokenService.Start(context.Background()); err != nil {
			logger.Error("failed to start token service", "error", err)
		} else {
			logger.Info("token service started")
		}
		defer tokenService.Stop()
		controllerClient, err = controllerclient.New(controllerclient.Config{
			APIURL:        kagentAPIURL,
			AgentName:     appName,
			TokenProvider: tokenService,
		})
		if err != nil {
			logger.Error("failed to create controller API client", "error", err, "url", kagentAPIURL)
			os.Exit(1)
		}
		defer func() {
			if err := controllerClient.Close(); err != nil {
				logger.Error("failed to close controller gRPC client", "error", err)
			}
		}()
	}

	// The executor needs a session service for its BeforeExecute callback
	// (session creation/lookup). This must be created before the executor.
	// AgentConfig.session_db_url selects the actor-local DurableDir store.
	sessionService, err := session.NewService(agentConfig.SessionDBURL)
	if err != nil {
		logger.Error("failed to open local session store", "error", err, "url", agentConfig.SessionDBURL)
		os.Exit(1)
	}
	switch sessionService.(type) {
	case *session.LocalSessionService:
		logger.Info("using local durable-dir session store", "url", agentConfig.SessionDBURL)
	default:
		logger.Info("no session DB configured, using in-memory session")
	}

	ctx := logging.IntoContext(context.Background(), logger)

	// Build memory service if configured.
	var memoryService *kagentmemory.KagentMemoryService
	if agentConfig.Memory != nil && controllerClient != nil {
		memSvc, err := kagentmemory.New(kagentmemory.Config{
			AgentName:        appName,
			ControllerClient: controllerClient,
			TTLDays:          agentConfig.Memory.TTLDays,
			EmbeddingConfig:  agentConfig.Memory.Embedding,
		})
		if err != nil {
			logger.Error("failed to create memory service", "error", err)
			os.Exit(1)
		}
		memoryService = memSvc
		logger.Info("memory service enabled", "app_name", appName)
	}

	runnerConfig, err := runnerpkg.CreateRunnerConfig(ctx, agentConfig, sessionService, appName, memoryService, controllerClient)
	if err != nil {
		logger.Error("failed to create Google ADK Runner config", "error", err)
		os.Exit(1)
	}

	stream := agentConfig.GetStream()
	executor := a2a.NewKAgentExecutor(a2a.KAgentExecutorConfig{
		RunnerConfig:   runnerConfig,
		SessionService: sessionService,
		Stream:         stream,
		AppName:        appName,
		Logger:         logger,
	})

	// Build the agent card.
	if agentCard == nil {
		agentCard = &a2atype.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
			Version:     "0.2.0",
			SupportedInterfaces: []*a2atype.AgentInterface{
				a2atype.NewAgentInterface("/", a2atype.TransportProtocolJSONRPC),
			},
		}
	}
	agentCard.Capabilities = a2atype.AgentCapabilities{
		Streaming: stream,
	}

	// Delegate the actor-local A2A server and task store to app.New.
	kagentApp, err := app.New(app.AppConfig{
		AgentCard:       *agentCard,
		Host:            *host,
		Port:            port,
		AppName:         appName,
		ShutdownTimeout: 5 * time.Second,
		Logger:          logger,
		Agent:           runnerConfig.Agent,
	}, executor)
	if err != nil {
		logger.Error("failed to create app", "error", err)
		os.Exit(1)
	}

	if err := kagentApp.Run(); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func deriveAppName(kagentName, kagentNamespace string, agentCard *a2atype.AgentCard, logger *slog.Logger) string {
	if kagentNamespace != "" && kagentName != "" {
		namespace := strings.ReplaceAll(kagentNamespace, "-", "_")
		name := strings.ReplaceAll(kagentName, "-", "_")
		appName := namespace + "__NS__" + name
		logger.Info("built app_name from environment variables",
			"kagent_namespace", kagentNamespace,
			"kagent_name", kagentName,
			"app_name", appName)
		return appName
	}

	if agentCard != nil && agentCard.Name != "" {
		logger.Info("using agent card name as app_name", "app_name", agentCard.Name)
		return agentCard.Name
	}

	logger.Info("using default app_name", "app_name", "go-adk-agent")
	return "go-adk-agent"
}
