package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	adkagent "google.golang.org/adk/v2/agent"

	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a/server"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/pkg/invocationauth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const (
	defaultPort            = "8080"
	defaultShutdownTimeout = 5 * time.Second
	defaultAppName         = "go-adk-agent"
)

// AppConfig holds configuration for a KAgent A2A application.
type AppConfig struct {
	// ControllerClient shares the existing controller channel with the TaskStore.
	// When nil, ControlPlaneURL configures a channel owned by this app.
	ControllerClient *controllerclient.Client
	ControlPlaneURL  string
	MaxContentLength *int64
	// AgentCard describes the agent's capabilities for A2A discovery.
	AgentCard a2atype.AgentCard

	// Host is the address to bind to. Empty string binds to all interfaces.
	Host string

	// Port is the port to listen on. Defaults to "8080".
	Port string

	// AppName identifies this application for session and tracing purposes.
	// Defaults to AgentCard.Name, then "go-adk-agent".
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
	server          *server.A2AServer
	logger          *slog.Logger
	ownedController *controllerclient.Client
}

// New creates a KAgentApp by wiring the provided executor with kagent
// infrastructure. The executor must implement a2asrv.AgentExecutor.
func New(cfg AppConfig, executor a2asrv.AgentExecutor) (*KAgentApp, error) {
	if executor == nil {
		return nil, fmt.Errorf("executor must not be nil")
	}
	if cfg.Logger == nil {
		logger, err := logging.New(os.Stderr, "info")
		if err != nil {
			return nil, fmt.Errorf("create logger: %w", err)
		}
		cfg.Logger = logger
	}

	cfg = applyDefaults(cfg)

	log := cfg.Logger

	app := &KAgentApp{logger: log}
	controller := cfg.ControllerClient
	if controller == nil {
		apiURL := cfg.ControlPlaneURL
		if apiURL == "" {
			return nil, fmt.Errorf("ControllerClient or ControlPlaneURL is required")
		}
		var err error
		controller, err = controllerclient.New(controllerclient.Config{APIURL: apiURL, AgentName: cfg.AppName})
		if err != nil {
			return nil, err
		}
		app.ownedController = controller
	}
	tasks := runtimetaskstore.New(controller, apia2a.RuntimeIdentityPath)
	runtimeExecutor := tasks.WrapExecutor(executor, cfg.Telemetry.Runtime, cfg.Flush)
	executor = runtimeExecutor
	// Append any caller-supplied handler options.
	handlerOpts := append(handlerOptions(tasks, runtimeExecutor, controller.GetAPIURL()), cfg.HandlerOpts...)

	serverConfig := server.ServerConfig{
		Host:             cfg.Host,
		Port:             cfg.Port,
		ShutdownTimeout:  cfg.ShutdownTimeout,
		HealthPaths:      cfg.HealthPaths,
		HealthHandler:    cfg.HealthHandler,
		Telemetry:        cfg.Telemetry,
		Flush:            cfg.Flush,
		MaxContentLength: cfg.MaxContentLength,
	}

	a2aServer, err := server.NewA2AServer(buildAgentCard(cfg), executor, log, serverConfig, handlerOpts...)
	if err != nil {
		if app.ownedController != nil {
			err = errors.Join(err, app.ownedController.Close())
		}
		return nil, fmt.Errorf("failed to create A2A server: %w", err)
	}
	app.server = a2aServer

	return app, nil
}

// handlerOptions coordinates native execution with SDK persistence and cleanup.
// The runtime interceptor admits one send; the SDK limiter frees only after settlement.
func handlerOptions(tasks *runtimetaskstore.Store, runtime a2asrv.CallInterceptor, controlPlaneURL string) []a2asrv.RequestHandlerOption {
	return []a2asrv.RequestHandlerOption{
		a2asrv.WithTaskStore(tasks),
		a2asrv.WithCallInterceptors(
			a2a.HITLActivationInterceptor(),
			a2a.UsageActivationInterceptor(),
			a2a.UserIDCallInterceptor(),
			invocationauth.PolicyCallInterceptor(controlPlaneURL),
			runtime,
		),
	}
}

// buildAgentCard returns the card the server serves. The HITL and usage extensions
// are declared for every app, whether or not an ADK agent was supplied for skill derivation.
func buildAgentCard(cfg AppConfig) a2atype.AgentCard {
	card := cfg.AgentCard
	a2a.EnsureHITLExtension(&card)
	a2a.EnsureUsageExtension(&card)
	if cfg.Agent != nil {
		a2a.EnrichAgentCard(&card, cfg.Agent)
	}
	return card
}

// Run starts the A2A server and blocks until a shutdown signal is received.
func (a *KAgentApp) Run() error {
	err := a.server.Run()
	if a.ownedController != nil {
		err = errors.Join(err, a.ownedController.Close())
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

// buildAppName supplies an identity for standalone applications.
func buildAppName(agentCard *a2atype.AgentCard) string {
	if agentCard != nil && agentCard.Name != "" {
		return agentCard.Name
	}

	return defaultAppName
}
