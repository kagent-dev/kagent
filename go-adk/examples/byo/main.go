// Package main demonstrates how to build a BYO (Bring Your Own) agent using
// the Go ADK's pkg/app builder with hardcoded agent configuration and
// Google ADK's ParallelAgent for concurrent sub-agent execution.
//
// Instead of loading config from files, this example builds an AgentConfig
// programmatically, creates two LLM sub-agents ("creative_writer" and
// "technical_writer"), wraps them in a ParallelAgent, and exposes the result
// as an A2A-compatible agent.
//
// The app builder automatically wires kagent infrastructure based on
// environment variables:
//
//   - KAGENT_URL: when set, enables remote session and task persistence via
//     the kagent controller API. Token auth is handled automatically.
//   - KAGENT_NAMESPACE / KAGENT_NAME: used to derive the app name for session
//     scoping. Falls back to the agent card name.
//   - PORT: the port to listen on (default "8080").
//
// Required environment variables:
//
//   - OPENAI_API_KEY: your OpenAI API key.
//
// Optional environment variables:
//
//   - MODEL_NAME: the OpenAI model to use (default "gpt-4o-mini").
//
// Run locally (standalone, no kagent):
//
//	OPENAI_API_KEY=sk-... go run ./examples/byo/
//
// Run with kagent persistence:
//
//	KAGENT_URL=http://kagent-controller:8080 OPENAI_API_KEY=sk-... go run ./examples/byo/
//
// Test with curl:
//
//	curl -s http://localhost:8082/.well-known/agent.json | jq .
package main

import (
	"log"
	"os"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go-adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/app"
	"github.com/kagent-dev/kagent/go-adk/pkg/models"
	"go.uber.org/zap"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/parallelagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
	adksession "google.golang.org/adk/session"
)

func main() {
	zapLogger, _ := zap.NewProduction()
	defer func() { _ = zapLogger.Sync() }()
	logger := zapr.NewLogger(zapLogger)

	modelName := os.Getenv("MODEL_NAME")
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	llmModel, err := models.NewOpenAIModelWithLogger(&models.OpenAIConfig{
		Model: modelName,
	}, logger)
	if err != nil {
		log.Fatalf("Failed to create LLM model: %v", err)
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
		log.Fatalf("Failed to create creative_writer agent: %v", err)
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
		log.Fatalf("Failed to create technical_writer agent: %v", err)
	}

	parallelAgent, err := parallelagent.New(parallelagent.Config{
		AgentConfig: adkagent.Config{
			Name:        "parallel_writer",
			Description: "Runs creative and technical writers in parallel on the same topic",
			SubAgents:   []adkagent.Agent{creativeWriter, technicalWriter},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create parallel agent: %v", err)
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
	executor := a2a.WrapExecutorQueue(adka2a.NewExecutor(execConfig))

	kagentApp, err := app.New(app.AppConfig{
		AgentCard: a2atype.AgentCard{
			Name:        "byo-parallel-agent",
			Description: "A BYO agent that runs creative and technical writers in parallel",
			Version:     "1.0.0",
			URL:         "http://localhost:8082",
			Capabilities: a2atype.AgentCapabilities{
				Streaming:              stream,
				StateTransitionHistory: true,
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
		Port:   "8082",
		Logger: logger,
		Agent:  parallelAgent,
	}, executor)
	if err != nil {
		log.Fatalf("Failed to create app: %v", err)
	}

	if err := kagentApp.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
