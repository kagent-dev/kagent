// Package main demonstrates declarative workflow agents: an AgentSystem
// document (agents.yaml) is compiled into a native ADK workflow agent by
// pkg/declarative and served over A2A.
//
// The document classifies requests with CEL nodes, calls customer tools on a
// remote MCP server, and stores tickets with the harness file tools. This
// program supplies the rest through a declarative.Registry: a Go function,
// the harness tools, and a model resolver that maps unregistered model names
// to OpenAI models. Use the "stub" model name in a document to run without
// credentials.
//
// CRM_MCP_URL selects the CRM MCP server; when it is unset the program serves
// a demo CRM on a loopback port.
//
// Deploy through a BYO Harness. Process settings are loaded from the root
// runtime object in /config/config.json. OPENAI_API_KEY remains a provider
// credential. An AgentTemplate that sets spec.workflowFrom replaces the
// embedded document: the controller validates it and delivers it in
// /config/config.json.
package main

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"

	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

//go:embed agents.yaml
var agentsDocument []byte

// newRegistry returns the local library of the document. workspace is the
// directory of the harness tools; empty uses the actor workspace.
func newRegistry(workspace string) (*declarative.Registry, error) {
	reg := declarative.NewRegistry()

	// Unregistered model names resolve to OpenAI models.
	reg.SetModelResolver(func(ctx context.Context, name string) (adkmodel.LLM, error) {
		return models.NewOpenAIModel(ctx, &models.OpenAIConfig{Model: name})
	})

	if err := reg.RegisterFunction("fallback", func(adkagent.Context, any) (declarative.Result, error) {
		return declarative.Output("I could not classify that request."), nil
	}); err != nil {
		return nil, err
	}
	// read_file, write_file, list_files and bash.
	if err := reg.RegisterHarnessTools(declarative.HarnessConfig{Workspace: workspace}); err != nil {
		return nil, err
	}
	return reg, nil
}

// loadDocument prefers the document the controller validated and delivered
// from AgentTemplate.spec.workflowFrom, and falls back to the embedded one.
func loadDocument(reg *declarative.Registry, delivered *configv1.ADKWorkflowConfig) (*declarative.Spec, error) {
	if delivered != nil {
		return reg.ParseConfig(delivered)
	}
	return reg.Parse(agentsDocument)
}

func main() {
	if err := config.MaterializeFromEnv("/config"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	compiled, err := config.LoadAgentConfig("/config/config.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runtime := config.ProcessConfig(compiled)
	logger, err := logging.New(os.Stderr, cmp.Or(runtime.LogLevel, "info"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	ctx := logging.IntoContext(context.Background(), logger)

	if _, set := os.LookupEnv(crmURLEnv); !set { //nolint:forbidigo // demo switch for the example's own MCP server
		url, stop, err := serveDemoCRM()
		if err != nil {
			logger.Error("failed to serve the demo CRM", "error", err)
			os.Exit(1)
		}
		defer stop()
		if err := os.Setenv(crmURLEnv, url); err != nil {
			logger.Error("failed to set the CRM URL", "error", err)
			os.Exit(1)
		}
		logger.Info("serving the demo CRM MCP server", "url", url)
	}

	reg, err := newRegistry("")
	if err != nil {
		logger.Error("failed to create registry", "error", err)
		os.Exit(1)
	}
	spec, err := loadDocument(reg, compiled.GetAgent().GetWorkflow())
	if err != nil {
		logger.Error("failed to load the agent document", "error", err)
		os.Exit(1)
	}
	system, err := declarative.Build(ctx, spec, reg)
	if err != nil {
		logger.Error("failed to build agents", "error", err)
		os.Exit(1)
	}
	defer system.Close()
	root := system.Root()

	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        root.Name(),
			Agent:          root,
			SessionService: adksession.InMemoryService(),
		},
	})

	kagentApp, err := app.New(app.AppConfig{ControlPlaneURL: runtime.ControlPlaneUrl, MaxContentLength: runtime.MaxContentLength,
		AgentCard: a2atype.AgentCard{
			Name:        root.Name(),
			Description: root.Description(),
			Version:     "1.0.0",
			SupportedInterfaces: []*a2atype.AgentInterface{
				a2atype.NewAgentInterface("http://localhost:8082", a2atype.TransportProtocolJSONRPC),
			},
			DefaultInputModes:  []string{"text/plain"},
			DefaultOutputModes: []string{"text/plain"},
			Skills: []a2atype.AgentSkill{{
				ID:          "support-triage",
				Name:        "Support triage",
				Description: "Classifies a request with CEL and routes it to escalation (saved with harness tools), a remote MCP billing lookup, a model answer, or a fallback",
			}},
		},
		Port:   fmt.Sprint(cmp.Or(runtime.GetPort(), 8082)),
		Logger: logger,
		Agent:  root,
	}, executor)
	if err != nil {
		logger.Error("failed to create app", "error", err)
		os.Exit(1)
	}
	if err := kagentApp.Run(); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
