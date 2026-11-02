// Package main is a generic BYO runtime for declarative workflow agents.
//
// It serves whatever AgentSystem document the controller delivers from
// AgentTemplate.spec.workflowFrom in /config/config.json. The image carries no
// workflow and no workflow-specific Go code; the registry holds only what any
// document can name:
//
//   - Models: every model name resolves to the AgentTemplate's ModelConfig, so
//     a document runs against whichever model the operator configured. Without
//     a ModelConfig the name is used as an OpenAI model. "stub" is always the
//     offline test model.
//   - Tools: the harness tools read_file, write_file, list_files and bash in
//     the actor workspace. MCP toolsets are declared in the document itself.
//
// Telemetry and A2A output follow the managed kagent runtime: delivered
// exporter settings and OTEL_* variables configure tracing, every turn is
// flushed before its final event, and each workflow node's reply is its own
// artifact carrying the node as author.
//
// Build the image with `make build-workflow-agent` and run it from a BYO
// Harness; see go/adk/examples/workflow for complete manifests.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"

	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/agent"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	processconfig "github.com/kagent-dev/kagent/go/core/pkg/config"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const configDir = "/config"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.MaterializeFromEnv(configDir); err != nil {
		return err
	}
	compiled, err := config.LoadAgentConfig(filepath.Join(configDir, "config.json"))
	if err != nil {
		return err
	}
	card, err := loadAgentCard(filepath.Join(configDir, "agent-card.json"))
	if err != nil {
		return err
	}
	runtime := config.ProcessConfig(compiled)
	logger, err := logging.New(os.Stderr, cmp.Or(runtime.LogLevel, "info"))
	if err != nil {
		return err
	}
	slog.SetDefault(logger)
	ctx := logging.IntoContext(context.Background(), logger)

	runtimeTelemetry := processconfig.RuntimeTelemetry(runtime.GetTelemetry()).WithEnvironment(os.Environ())
	runtimeTelemetry.Runtime = tracing.RuntimeADKGo
	runtimeTelemetry.AgentName = runtime.Name
	runtimeTelemetry.AgentNamespace = cmp.Or(runtime.Namespace, "default")
	providers, err := telemetry.Init(ctx, runtimeTelemetry)
	if err != nil {
		logger.Error("failed to initialize telemetry", "error", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := providers.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shut down telemetry", "error", err)
		}
	}()

	agentConfig := compiled.GetAgent()
	reg, err := newRegistry(agentConfig.GetModel(), "")
	if err != nil {
		return fmt.Errorf("create registry: %w", err)
	}
	spec, err := loadDocument(reg, agentConfig.GetWorkflow())
	if err != nil {
		return err
	}
	system, err := declarative.Build(ctx, spec, reg)
	if err != nil {
		return fmt.Errorf("build workflow: %w", err)
	}
	defer system.Close()
	root := system.Root()

	sessions, err := session.NewService(agentConfig.GetSessionDbUrl())
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	if sessions == nil {
		sessions = adksession.InMemoryService()
	}
	appName := appName(runtime.Namespace, runtime.Name, root.Name())
	if runtimeTelemetry.AgentName == "" {
		runtimeTelemetry.AgentName = appName
	}
	executor, err := a2a.NewKAgentExecutor(a2a.KAgentExecutorConfig{
		RunnerConfig:   runner.Config{AppName: appName, Agent: root, SessionService: sessions},
		SessionService: sessions,
		Stream:         agentConfig.GetStream(),
		AppName:        appName,
		Logger:         logger,
		Flush:          providers.ForceFlush,
		AgentName:      runtimeTelemetry.AgentName,
	})
	if err != nil {
		return fmt.Errorf("create A2A executor: %w", err)
	}

	kagentApp, err := app.New(app.AppConfig{
		ControlPlaneURL:  runtime.ControlPlaneUrl,
		MaxContentLength: runtime.MaxContentLength,
		AgentCard:        agentCardFor(card, root),
		Host:             runtime.Host,
		Port:             fmt.Sprint(cmp.Or(runtime.GetPort(), 8082)),
		AppName:          appName,
		ShutdownTimeout:  5 * time.Second,
		Logger:           logger,
		Agent:            root,
		Telemetry:        runtimeTelemetry,
		Flush:            providers.ForceFlush,
	}, executor)
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}
	return kagentApp.Run()
}

// newRegistry returns the generic library every document may use. model is the
// AgentTemplate's ModelConfig, nil when it has none; workspace is the directory
// of the harness tools, empty for the actor workspace.
func newRegistry(model *configv1.ADKModelConfig, workspace string) (*declarative.Registry, error) {
	reg := declarative.NewRegistry()
	reg.SetModelResolver(modelResolver(model, agent.CreateLLM))
	if err := reg.RegisterHarnessTools(declarative.HarnessConfig{Workspace: workspace}); err != nil {
		return nil, err
	}
	return reg, nil
}

// llmFactory creates the model of a ModelConfig.
type llmFactory func(context.Context, *configv1.ADKModelConfig) (adkmodel.LLM, error)

// modelResolver maps every model name a document uses to the ModelConfig's
// model, created once and shared by all nodes. Without a ModelConfig the name
// is an OpenAI model.
func modelResolver(model *configv1.ADKModelConfig, create llmFactory) declarative.ModelResolver {
	if model == nil {
		return func(ctx context.Context, name string) (adkmodel.LLM, error) {
			return models.NewOpenAIModel(ctx, &models.OpenAIConfig{Model: name})
		}
	}
	var (
		mu  sync.Mutex
		llm adkmodel.LLM
	)
	return func(ctx context.Context, _ string) (adkmodel.LLM, error) {
		mu.Lock()
		defer mu.Unlock()
		if llm != nil {
			return llm, nil
		}
		created, err := create(ctx, model)
		if err != nil {
			return nil, fmt.Errorf("create model from ModelConfig: %w", err)
		}
		llm = created
		return llm, nil
	}
}

// loadDocument parses the document the controller validated and delivered.
func loadDocument(reg *declarative.Registry, delivered *configv1.ADKWorkflowConfig) (*declarative.Spec, error) {
	if delivered == nil {
		return nil, errors.New("no workflow document delivered: set spec.workflowFrom on the AgentTemplate")
	}
	spec, err := reg.ParseConfig(delivered)
	if err != nil {
		return nil, fmt.Errorf("load workflow document: %w", err)
	}
	return spec, nil
}

// loadAgentCard reads the delivered agent card; a missing file is not an error.
func loadAgentCard(path string) (*a2atype.AgentCard, error) {
	card, err := config.LoadAgentCard(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return card, err
}

// agentCardFor returns the delivered card, or one describing the workflow root.
// Task events always stream, whether or not model responses do.
func agentCardFor(delivered *a2atype.AgentCard, root adkagent.Agent) a2atype.AgentCard {
	card := a2atype.AgentCard{
		Name:        root.Name(),
		Description: root.Description(),
		Version:     "1.0.0",
		SupportedInterfaces: []*a2atype.AgentInterface{
			a2atype.NewAgentInterface("/", a2atype.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}
	if delivered != nil {
		card = *delivered
	}
	card.Capabilities.Streaming = true
	return card
}

// appName is the ADK application name of the agent, namespace__NS__name as in
// the managed runtime, or the workflow root's name outside a cluster.
func appName(namespace, name, rootName string) string {
	if namespace == "" || name == "" {
		return rootName
	}
	return strings.ReplaceAll(namespace, "-", "_") + "__NS__" + strings.ReplaceAll(name, "-", "_")
}
