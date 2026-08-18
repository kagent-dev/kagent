package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a/server"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	adkagent "google.golang.org/adk/v2/agent"
)

const (
	defaultPort            = "8080"
	defaultShutdownTimeout = 5 * time.Second
	defaultAppName         = "go-adk-agent"
)

// AppConfig holds configuration for a KAgent A2A application.
type AppConfig struct {
	// AgentCard describes the agent's capabilities for A2A discovery.
	AgentCard a2atype.AgentCard

	// Host is the address to bind to. Empty string binds to all interfaces.
	Host string

	// Port is the port to listen on. Defaults to the PORT env var, then "8080".
	Port string

	// KAgentGRPCURL is the KAgent controller gRPC target for remote session/task persistence.
	// Defaults to the KAGENT_GRPC_URL env var. When empty, the app uses no remote persistence.
	KAgentGRPCURL string

	// AppName identifies this application for session and tracing purposes.
	// Defaults to KAGENT_NAMESPACE__NS__KAGENT_NAME from env, then AgentCard.Name,
	// then "go-adk-agent".
	AppName string

	// ShutdownTimeout is the graceful shutdown timeout. Defaults to 5 seconds.
	ShutdownTimeout time.Duration

	// Logger is the structured logger. If nil, a production zap logger is created.
	Logger logr.Logger

	// ControllerClient overrides the authenticated gRPC client used for KAgent
	// persistence. When nil and KAgentGRPCURL is set, the builder creates and owns
	// a client with Kubernetes token authentication.
	ControllerClient *controllerclient.Client

	// HandlerOpts are additional a2asrv.RequestHandlerOption values appended
	// after the ones the builder creates (task store, push notifications, etc.).
	HandlerOpts []a2asrv.RequestHandlerOption

	// Agent is the ADK agent used to enrich the agent card with skills via
	// adka2a.BuildAgentSkills. Optional; when nil, the card is used as-is.
	Agent adkagent.Agent
}

// KAgentApp wires an AgentExecutor with kagent infrastructure (auth, session,
// task store, A2A server) so that BYO users only need to provide their executor.
type KAgentApp struct {
	server               *server.A2AServer
	tokenService         *auth.KAgentTokenService
	controllerClient     *controllerclient.Client
	ownsControllerClient bool
	sessionService       *session.KAgentSessionService
	logger               logr.Logger
}

// New creates a KAgentApp by wiring the provided executor with kagent
// infrastructure. The executor must implement a2asrv.AgentExecutor.
func New(cfg AppConfig, executor a2asrv.AgentExecutor) (*KAgentApp, error) {
	if executor == nil {
		return nil, fmt.Errorf("executor must not be nil")
	}

	cfg = applyDefaults(cfg)

	log := cfg.Logger

	app := &KAgentApp{
		logger: log,
	}

	// Wire remote infrastructure when a controller gRPC target or client is configured.
	var handlerOpts []a2asrv.RequestHandlerOption
	controllerClient := cfg.ControllerClient
	if controllerClient != nil || cfg.KAgentGRPCURL != "" {
		if controllerClient == nil {
			tokenService := auth.NewKAgentTokenService(cfg.AppName)
			if err := tokenService.Start(context.Background()); err != nil {
				log.Error(err, "Failed to start token service")
			} else {
				log.Info("Token service started")
			}
			app.tokenService = tokenService
			var err error
			controllerClient, err = controllerclient.New(controllerclient.Config{
				Target:        cfg.KAgentGRPCURL,
				AgentName:     cfg.AppName,
				TokenProvider: tokenService,
			})
			if err != nil {
				app.stop()
				return nil, fmt.Errorf("create controller gRPC client: %w", err)
			}
			app.controllerClient = controllerClient
			app.ownsControllerClient = true
		}

		sessionSvc := session.NewKAgentSessionService(controllerClient)
		app.sessionService = sessionSvc
		log.Info("Using KAgent gRPC session service", "target", cfg.KAgentGRPCURL)
	} else {
		log.Info("No KAgent gRPC target configured, using in-memory session")
	}

	if controllerClient != nil {
		handlerOpts = append(handlerOpts, a2asrv.WithTaskStore(taskstore.NewKAgentTaskStore(controllerClient)))
		log.Info("Using KAgent gRPC task store", "target", cfg.KAgentGRPCURL)
	} else {
		log.Info("No task store configured, using in-memory task storage")
	}

	// Activate the optional HITL extension and resolve the authenticated user.
	handlerOpts = append(handlerOpts, a2asrv.WithCallInterceptors(
		a2a.HITLActivationInterceptor(),
		a2a.UserIDCallInterceptor(),
	))

	// Append any caller-supplied handler options.
	handlerOpts = append(handlerOpts, cfg.HandlerOpts...)

	a2a.EnsureHITLExtension(&cfg.AgentCard)

	// Enrich agent card with skills derived from the ADK agent.
	if cfg.Agent != nil {
		a2a.EnrichAgentCard(&cfg.AgentCard, cfg.Agent)
	}

	serverConfig := server.ServerConfig{
		Host:            cfg.Host,
		Port:            cfg.Port,
		ShutdownTimeout: cfg.ShutdownTimeout,
	}

	a2aServer, err := server.NewA2AServer(cfg.AgentCard, executor, log, serverConfig, handlerOpts...)
	if err != nil {
		app.stop()
		return nil, fmt.Errorf("failed to create A2A server: %w", err)
	}
	app.server = a2aServer

	return app, nil
}

// Run starts the A2A server and blocks until a shutdown signal is received.
func (a *KAgentApp) Run() error {
	defer a.stop()
	return a.server.Run()
}

// SessionService returns the wired session service. BYO executors that need
// session persistence can use this. Returns nil when controller gRPC is not configured.
func (a *KAgentApp) SessionService() *session.KAgentSessionService {
	return a.sessionService
}

// Logger returns the logger used by this app.
func (a *KAgentApp) Logger() logr.Logger {
	return a.logger
}

// stop cleans up resources.
func (a *KAgentApp) stop() {
	if a.ownsControllerClient && a.controllerClient != nil {
		if err := a.controllerClient.Close(); err != nil {
			a.logger.Error(err, "Failed to close controller gRPC client")
		}
	}
	if a.tokenService != nil {
		a.tokenService.Stop()
	}
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg AppConfig) AppConfig {
	if cfg.Port == "" {
		cfg.Port = os.Getenv("PORT")
	}
	if cfg.Port == "" {
		cfg.Port = defaultPort
	}

	if cfg.KAgentGRPCURL == "" {
		cfg.KAgentGRPCURL = os.Getenv("KAGENT_GRPC_URL")
	}

	if cfg.AppName == "" {
		cfg.AppName = buildAppName(&cfg.AgentCard)
	}

	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}

	if cfg.Logger.GetSink() == nil {
		cfg.Logger = newDefaultLogger()
	}

	// Ensure the agent card always advertises at least one interface so A2A
	// clients can select a compatible endpoint/transport.
	if len(cfg.AgentCard.SupportedInterfaces) == 0 {
		cfg.AgentCard.SupportedInterfaces = []*a2atype.AgentInterface{
			a2atype.NewAgentInterface("/", a2atype.TransportProtocolJSONRPC),
		}
	}

	return cfg
}

// buildAppName derives the app name from environment variables or agent card,
// following the same convention as the Python KAgentConfig.
func buildAppName(agentCard *a2atype.AgentCard) string {
	kagentName := os.Getenv("KAGENT_NAME")
	kagentNamespace := os.Getenv("KAGENT_NAMESPACE")

	if kagentNamespace != "" && kagentName != "" {
		namespace := strings.ReplaceAll(kagentNamespace, "-", "_")
		name := strings.ReplaceAll(kagentName, "-", "_")
		return namespace + "__NS__" + name
	}

	if agentCard != nil && agentCard.Name != "" {
		return agentCard.Name
	}

	return defaultAppName
}

// newDefaultLogger creates a production zap logger wrapped as logr.Logger.
func newDefaultLogger() logr.Logger {
	zapConfig := zap.NewProductionConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	zapConfig.EncoderConfig.TimeKey = "timestamp"
	zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	zapLogger, err := zapConfig.Build()
	if err != nil {
		devConfig := zap.NewDevelopmentConfig()
		devConfig.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		zapLogger, _ = devConfig.Build()
	}
	return zapr.NewLogger(zapLogger)
}
