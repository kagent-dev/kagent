package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a/server"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	adkagent "google.golang.org/adk/agent"
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

	// KAgentURL is the KAgent controller URL for remote session/task persistence.
	// Defaults to the KAGENT_URL env var. When empty, the app uses no remote persistence.
	KAgentURL string

	// AppName identifies this application for session and tracing purposes.
	// Defaults to KAGENT_NAMESPACE__NS__KAGENT_NAME from env, then AgentCard.Name,
	// then "go-adk-agent".
	AppName string

	// ShutdownTimeout is the graceful shutdown timeout. Defaults to 5 seconds.
	ShutdownTimeout time.Duration

	// Logger is the structured logger. If nil, a production zap logger is created.
	Logger logr.Logger

	// HTTPClient overrides the default authenticated HTTP client used for
	// KAgent API calls (task store, session service). When nil and KAgentURL
	// is set, the builder creates a new client with K8s token auth.
	// Provide this when you already manage token auth yourself (e.g. the
	// declarative image creates its own token service for the executor).
	HTTPClient *http.Client

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
	server         *server.A2AServer
	tokenService   *auth.KAgentTokenService
	sessionService session.SessionService
	logger         logr.Logger
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

	// Wire remote infrastructure when KAgentURL is configured.
	var handlerOpts []a2asrv.RequestHandlerOption
	if cfg.KAgentURL != "" {
		httpClient := cfg.HTTPClient
		if httpClient == nil {
			tokenService := auth.NewKAgentTokenService(cfg.AppName)
			if err := tokenService.Start(context.Background()); err != nil {
				log.Error(err, "Failed to start token service")
			} else {
				log.Info("Token service started")
			}
			app.tokenService = tokenService
			httpClient = newHTTPClient(tokenService)
		}

		sessionSvc := session.NewKAgentSessionService(cfg.KAgentURL, httpClient)
		app.sessionService = sessionSvc
		log.Info("Using KAgent session service", "url", cfg.KAgentURL)

		taskStoreInstance := taskstore.NewKAgentTaskStoreWithClient(cfg.KAgentURL, httpClient)
		taskStoreAdapter := taskstore.NewA2ATaskStoreAdapter(taskStoreInstance)
		handlerOpts = append(handlerOpts, a2asrv.WithTaskStore(taskStoreAdapter))
		log.Info("Using KAgent task store", "url", cfg.KAgentURL)
	} else {
		log.Info("No KAgentURL configured, using in-memory session and no task persistence")
	}

	// Append any caller-supplied handler options.
	handlerOpts = append(handlerOpts, cfg.HandlerOpts...)

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
// session persistence can use this. Returns nil when KAgentURL is not configured.
func (a *KAgentApp) SessionService() session.SessionService {
	return a.sessionService
}

// Logger returns the logger used by this app.
func (a *KAgentApp) Logger() logr.Logger {
	return a.logger
}

// stop cleans up resources.
func (a *KAgentApp) stop() {
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

	if cfg.KAgentURL == "" {
		cfg.KAgentURL = os.Getenv("KAGENT_URL")
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

// newHTTPClient creates an HTTP client with optional token injection.
func newHTTPClient(tokenService *auth.KAgentTokenService) *http.Client {
	if tokenService != nil {
		return auth.NewHTTPClientWithToken(tokenService)
	}
	return &http.Client{Timeout: 30 * time.Second}
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
