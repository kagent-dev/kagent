package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a/server"
	localtaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
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

	// AppName identifies this application for session and tracing purposes.
	// Defaults to KAGENT_NAMESPACE__NS__KAGENT_NAME from env, then AgentCard.Name,
	// then "go-adk-agent".
	AppName string

	// ShutdownTimeout is the graceful shutdown timeout. Defaults to 5 seconds.
	ShutdownTimeout time.Duration

	// Logger is the structured logger. If nil, a production zap logger is created.
	Logger logr.Logger

	// HandlerOpts are additional a2asrv.RequestHandlerOption values appended
	// after the ones the builder creates (task store, push notifications, etc.).
	HandlerOpts []a2asrv.RequestHandlerOption

	// Agent is the ADK agent used to enrich the agent card with skills via
	// adka2a.BuildAgentSkills. Optional; when nil, the card is used as-is.
	Agent adkagent.Agent

	// SessionDBURL locates actor-local durable storage. When set, A2A tasks are
	// persisted beside the ADK session database so HITL tasks survive suspension.
	SessionDBURL string
}

// KAgentApp wires an AgentExecutor with kagent's A2A server.
type KAgentApp struct {
	server *server.A2AServer
	logger logr.Logger
}

type seedTaskInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	store a2ataskstore.Store
}

func (i seedTaskInterceptor) Before(ctx context.Context, _ *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	if req == nil {
		return ctx, nil, nil
	}
	send, ok := req.Payload.(*a2atype.SendMessageRequest)
	if !ok || send.Message == nil || send.Message.TaskID == "" {
		return ctx, nil, nil
	}
	if _, err := i.store.Get(ctx, send.Message.TaskID); err == nil {
		return ctx, nil, nil
	} else if !errors.Is(err, a2atype.ErrTaskNotFound) {
		return ctx, nil, fmt.Errorf("load actor task: %w", err)
	}
	if _, err := i.store.Create(ctx, a2atype.NewSubmittedTask(send.Message, send.Message)); err != nil && !errors.Is(err, a2ataskstore.ErrTaskAlreadyExists) {
		return ctx, nil, fmt.Errorf("seed actor task: %w", err)
	}
	return ctx, nil, nil
}

// New creates a KAgentApp by wiring the provided executor with kagent
// infrastructure. The executor must implement a2asrv.AgentExecutor.
func New(cfg AppConfig, executor a2asrv.AgentExecutor) (*KAgentApp, error) {
	if executor == nil {
		return nil, fmt.Errorf("executor must not be nil")
	}

	cfg = applyDefaults(cfg)

	log := cfg.Logger

	app := &KAgentApp{logger: log}
	authenticator := a2asrv.NewTaskStoreAuthenticator()
	var tasks a2ataskstore.Store
	if cfg.SessionDBURL == "" {
		tasks = a2ataskstore.NewInMemory(&a2ataskstore.InMemoryStoreConfig{Authenticator: authenticator})
	} else {
		var err error
		tasks, err = localtaskstore.New(cfg.SessionDBURL, authenticator)
		if err != nil {
			return nil, fmt.Errorf("open local task store: %w", err)
		}
	}
	handlerOpts := []a2asrv.RequestHandlerOption{a2asrv.WithTaskStore(tasks)}

	// The private runtime receives a gateway-assigned ID for a new task. Seed it
	// locally so upstream A2A does not mistake that ID for a continuation.
	handlerOpts = append(handlerOpts, a2asrv.WithCallInterceptors(
		a2a.HITLActivationInterceptor(),
		a2a.UserIDCallInterceptor(),
		seedTaskInterceptor{store: tasks},
	))

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
	return a.server.Run()
}

// Logger returns the logger used by this app.
func (a *KAgentApp) Logger() logr.Logger {
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
