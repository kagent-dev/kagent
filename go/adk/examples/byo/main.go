// Package main demonstrates how to build a BYO (Bring Your Own) agent using
// the Go ADK's pkg/app builder with hardcoded agent configuration and
// Google ADK's ParallelAgent for concurrent sub-agent execution.
//
// Instead of loading config from files, this example builds an AgentConfig
// programmatically, creates two LLM sub-agents ("creative_writer" and
// "technical_writer"), wraps them in a ParallelAgent, and exposes the result
// as an A2A-compatible agent.
//
// Deploy through a BYO Harness. Process settings are loaded from the root
// runtime object in /config/config.json. OPENAI_API_KEY remains a provider
// credential and MODEL_NAME selects this example's model (default gpt-4o-mini).
package main

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent/workflowagents/parallelagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"

	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

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

	modelName := os.Getenv("MODEL_NAME") //nolint:forbidigo // Application-specific setting in this standalone BYO example.
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	ctx := logging.IntoContext(context.Background(), logger)
	llmModel, err := models.NewOpenAIModel(ctx, &models.OpenAIConfig{
		Model: modelName,
	})
	if err != nil {
		logger.Error("failed to create LLM model", "error", err)
		os.Exit(1)
	}

	creativeWriter, err := llmagent.New(llmagent.Config{
		Name:        "creative_writer",
		Description: "Writes creative, engaging content with storytelling flair",
		Instruction: "You are a creative writer. Given the user's topic, write a short, " +
			"engaging paragraph with vivid language and storytelling elements. " +
			"Keep it under 100 words.",
		Model: llmModel,
	})
	if err != nil {
		logger.Error("failed to create creative writer agent", "error", err)
		os.Exit(1)
	}

	technicalWriter, err := llmagent.New(llmagent.Config{
		Name:        "technical_writer",
		Description: "Writes clear, precise technical explanations",
		Instruction: "You are a technical writer. Given the user's topic, write a short, " +
			"clear technical explanation with precise language. " +
			"Keep it under 100 words.",
		Model: llmModel,
	})
	if err != nil {
		logger.Error("failed to create technical writer agent", "error", err)
		os.Exit(1)
	}

	parallelAgent, err := parallelagent.New(parallelagent.Config{
		AgentConfig: adkagent.Config{
			Name:        "parallel_writer",
			Description: "Runs creative and technical writers in parallel on the same topic",
			SubAgents:   []adkagent.Agent{creativeWriter, technicalWriter},
		},
	})
	if err != nil {
		logger.Error("failed to create parallel agent", "error", err)
		os.Exit(1)
	}

	runnerConfig := runner.Config{
		AppName:        "byo-parallel-agent",
		Agent:          parallelAgent,
		SessionService: adksession.InMemoryService(),
	}

	stream := true
	var runConfig adkagent.RunConfig
	runConfig.StreamingMode = adkagent.StreamingModeSSE

	execConfig := adka2a.ExecutorConfig{
		RunnerConfig: runnerConfig,
		RunConfig:    runConfig,
	}
	executor := adka2a.NewExecutor(execConfig)

	kagentApp, err := app.New(app.AppConfig{ControlPlaneURL: runtime.ControlPlaneUrl, MaxContentLength: runtime.MaxContentLength,
		AgentCard: a2atype.AgentCard{
			Name:        "byo-parallel-agent",
			Description: "A BYO agent that runs creative and technical writers in parallel",
			Version:     "1.0.0",
			SupportedInterfaces: []*a2atype.AgentInterface{
				a2atype.NewAgentInterface("http://localhost:8082", a2atype.TransportProtocolJSONRPC),
			},
			Capabilities: a2atype.AgentCapabilities{
				Streaming: stream,
			},
			DefaultInputModes:  []string{"text/plain"},
			DefaultOutputModes: []string{"text/plain"},
			Skills: []a2atype.AgentSkill{
				{
					ID:          "parallel-write",
					Name:        "Parallel Write",
					Description: "Writes about a topic from both creative and technical perspectives simultaneously",
				},
			},
		},
		Port:   fmt.Sprint(cmp.Or(runtime.GetPort(), 8082)),
		Logger: logger,
		Agent:  parallelAgent,
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
