package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a/server"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	adkagent "google.golang.org/adk/v2/agent"
)

const (
	defaultPort            = "8080"
	defaultShutdownTimeout = 5 * time.Second
	defaultAppName         = "go-adk-agent"
)

// AppConfig holds configuration for a KAgent A2A application.
type AppConfig struct {
	// ControllerClient shares the existing controller channel with the TaskStore.
	// When nil, KAGENT_API_URL configures a channel owned by this app. Without
	// either, the app runs locally with an in-memory TaskStore.
	ControllerClient *controllerclient.Client
	// AgentCard describes the agent's capabilities for A2A discovery.
	AgentCard a2atype.AgentCard

	// Host is the address to bind to. Empty string binds to all interfaces.
	Host string

	// Port is the port to listen on. Defaults to the PORT env var, then "8080".
	Port string

	// AppName identifies this application for session and tracing purposes.
	// Defaults to KAGENT_NAMESPACE__NS__KAGENT_NAME from env, then AgentCard.Name,
	// then "go-adk-agent".
	AppName string

	// ShutdownTimeout is the graceful shutdown timeout. Defaults to 5 seconds.
	ShutdownTimeout time.Duration

	// HealthPaths are literal exact paths served by HealthHandler and excluded from tracing.
	HealthPaths   []string
	HealthHandler http.Handler

	// Logger is the structured logger. If nil, a JSON logger is created.
	Logger *slog.Logger

	// HandlerOpts are additional a2asrv.RequestHandlerOption values appended
	// after the ones the builder creates (task store, push notifications, etc.).
	HandlerOpts []a2asrv.RequestHandlerOption

	// Agent is the ADK agent used to enrich the agent card with skills via
	// adka2a.BuildAgentSkills. Optional; when nil, the card is used as-is.
	Agent adkagent.Agent

	// Telemetry is the compiler-owned telemetry contract for this runtime. Its
	// static identity is stamped on every invocation span. The zero value
	// leaves invocation spans without a runtime or agent identity.
	Telemetry tracing.RuntimeTelemetry
	Flush     func(context.Context) error
}

// KAgentApp wires an AgentExecutor with kagent's A2A server.
type KAgentApp struct {
	server     *server.A2AServer
	logger     *slog.Logger
	closeTasks func() error
}

// New creates a KAgentApp by wiring the provided executor with kagent
// infrastructure. The executor must implement a2asrv.AgentExecutor.
func New(cfg AppConfig, executor a2asrv.AgentExecutor) (*KAgentApp, error) {
	if executor == nil {
		return nil, fmt.Errorf("executor must not be nil")
	}
	if cfg.Logger == nil {
		logger, err := logging.NewFromEnv(os.Stderr)
		if err != nil {
			return nil, fmt.Errorf("parse LOG_LEVEL: %w", err)
		}
		cfg.Logger = logger
	}

	cfg = applyDefaults(cfg)

	log := cfg.Logger

	app := &KAgentApp{logger: log}
	var tasks a2ataskstore.Store = a2ataskstore.NewInMemory(&a2ataskstore.InMemoryStoreConfig{Authenticator: a2asrv.NewTaskStoreAuthenticator()})
	var admission a2asrv.CallInterceptor = a2asrv.PassthroughCallInterceptor{}
	controller := cfg.ControllerClient
	ownedController := false
	if controller == nil && os.Getenv("KAGENT_API_URL") != "" {
		var err error
		controller, err = controllerclient.New(controllerclient.Config{APIURL: os.Getenv("KAGENT_API_URL"), AgentName: cfg.AppName})
		if err != nil {
			return nil, err
		}
		ownedController = true
	}
	if controller != nil {
		remote := runtimetaskstore.New(controller, apia2a.RuntimeIdentityPath, executor)
		tasks, admission = remote, remote
		executor = remote.WrapExecutor(executor)
		app.closeTasks = func() error {
			if ownedController {
				return controller.Close()
			}
			return nil
		}
	}
	handlerOpts := []a2asrv.RequestHandlerOption{
		a2asrv.WithTaskStore(tasks),
		a2asrv.WithConcurrencyConfig(limiter.ConcurrencyConfig{MaxExecutions: 1}),
	}

	// Runtime admission supplies the task and version before SDK execution.
	handlerOpts = append(handlerOpts, a2asrv.WithCallInterceptors(
		a2a.HITLActivationInterceptor(),
		a2a.UserIDCallInterceptor(),
		admission,
	))

	// Append any caller-supplied handler options.
	handlerOpts = append(handlerOpts, cfg.HandlerOpts...)

	serverConfig := server.ServerConfig{
		Host:            cfg.Host,
		Port:            cfg.Port,
		ShutdownTimeout: cfg.ShutdownTimeout,
		HealthPaths:     cfg.HealthPaths,
		HealthHandler:   cfg.HealthHandler,
		Telemetry:       cfg.Telemetry,
		Flush:           cfg.Flush,
	}

	a2aServer, err := server.NewA2AServer(buildAgentCard(cfg), executor, log, serverConfig, handlerOpts...)
	if err != nil {
		if app.closeTasks != nil {
			err = errors.Join(err, app.closeTasks())
		}
		return nil, fmt.Errorf("failed to create A2A server: %w", err)
	}
	app.server = a2aServer

	return app, nil
}

// buildAgentCard returns the card the server serves. The HITL extension is declared
// for every app, whether or not an ADK agent was supplied for skill derivation.
func buildAgentCard(cfg AppConfig) a2atype.AgentCard {
	card := cfg.AgentCard
	a2a.EnsureHITLExtension(&card)
	if cfg.Agent != nil {
		a2a.EnrichAgentCard(&card, cfg.Agent)
	}
	return card
}

// Run starts the A2A server and blocks until a shutdown signal is received.
func (a *KAgentApp) Run() error {
	err := a.server.Run()
	if a.closeTasks != nil {
		err = errors.Join(err, a.closeTasks())
	}
	return err
}

// Logger returns the logger used by this app.
func (a *KAgentApp) Logger() *slog.Logger {
	return a.logger
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg AppConfig) AppConfig {
	if cfg.Port == "" {
		cfg.Port = os.Getenv("PORT")
	}
	if cfg.Port == "" {
		cfg.Port = defaultPort
	}

	if cfg.AppName == "" {
		cfg.AppName = buildAppName(&cfg.AgentCard)
	}

	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
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
