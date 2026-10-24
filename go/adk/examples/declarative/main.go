// Package main demonstrates declarative workflow agents: an AgentSystem
// document (agents.yaml) is compiled into a native ADK workflow agent by
// pkg/declarative and served over A2A.
//
// The document only names behavior; this program supplies it through a
// declarative.Registry: Go functions, a tool, and a model resolver that maps
// unregistered model names to OpenAI models. Use the "stub" model name in a
// document to run without credentials.
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
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

//go:embed agents.yaml
var agentsDocument []byte

type lookupArgs struct {
	CustomerID string `json:"customer_id"`
}

type customer struct {
	ID   string `json:"id"`
	Plan string `json:"plan"`
}

func newRegistry() (*declarative.Registry, error) {
	reg := declarative.NewRegistry()

	// Unregistered model names resolve to OpenAI models.
	reg.SetModelResolver(func(ctx context.Context, name string) (adkmodel.LLM, error) {
		return models.NewOpenAIModel(ctx, &models.OpenAIConfig{Model: name})
	})

	lookup, err := functiontool.New(functiontool.Config{
		Name:        "lookup_customer",
		Description: "Looks up a customer by id.",
	}, func(_ adkagent.Context, args lookupArgs) (customer, error) {
		return customer{ID: args.CustomerID, Plan: "pro"}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create tool lookup_customer: %w", err)
	}

	for name, fn := range map[string]declarative.Func{
		"classify":   classify,
		"bill_reply": billReply,
		"fallback": func(adkagent.Context, any) (declarative.Result, error) {
			return declarative.Output("I could not classify that request."), nil
		},
	} {
		if err := reg.RegisterFunction(name, fn); err != nil {
			return nil, err
		}
	}
	if err := reg.RegisterTool(lookup); err != nil {
		return nil, err
	}
	return reg, nil
}

// classify routes on the first word of the user message. BILLING requests
// carry the tool arguments their successor expects.
func classify(ctx adkagent.Context, _ any) (declarative.Result, error) {
	var text strings.Builder
	if uc := ctx.UserContent(); uc != nil {
		for _, p := range uc.Parts {
			if p != nil {
				text.WriteString(p.Text)
			}
		}
	}
	switch first, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(text.String())), " "); first {
	case "billing":
		return declarative.Route(map[string]any{"customer_id": "c-42"}, "BILLING"), nil
	case "question":
		return declarative.Route(text.String(), "QUESTION"), nil
	}
	return declarative.Route(text.String(), "UNMATCHED"), nil
}

func billReply(_ adkagent.Context, in any) (declarative.Result, error) {
	return declarative.Output(fmt.Sprintf("Customer record: %v", in)), nil
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

	reg, err := newRegistry()
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
				Description: "Routes a request to billing lookup, a model answer, or a fallback",
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
