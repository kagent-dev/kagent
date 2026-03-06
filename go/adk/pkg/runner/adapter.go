package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/agent"
	"github.com/kagent-dev/kagent/go/adk/pkg/compaction"
	kagentmemory "github.com/kagent-dev/kagent/go/adk/pkg/memory"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/api/adk"
	adkmemory "google.golang.org/adk/memory"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
)

func agentNameFromAppName(appName string) string {
	if idx := strings.LastIndex(appName, "__NS__"); idx >= 0 {
		return appName[idx+len("__NS__"):]
	}
	return appName
}

// CreateRunnerConfig creates a runner.Config suitable for use with adka2a.Executor.
func CreateRunnerConfig(ctx context.Context, agentConfig *adk.AgentConfig, sessionService session.SessionService, appName string) (runner.Config, error) {
	adkAgent, err := agent.CreateGoogleADKAgent(ctx, agentConfig, agentNameFromAppName(appName))
	if err != nil {
		return runner.Config{}, fmt.Errorf("failed to create agent: %w", err)
	}

	var adkSessionService adksession.Service
	if sessionService != nil {
		adkSessionService = session.NewSessionServiceAdapter(sessionService)
	} else {
		adkSessionService = adksession.InMemoryService()
	}

	// Wrap session service with compaction if context compression is configured
	if agentConfig.ContextConfig != nil && agentConfig.ContextConfig.Compaction != nil {
		compactionConfig := compaction.Config{
			Enabled:            true,
			CompactionInterval: 5, // Default
			OverlapSize:        2, // Default
		}

		if agentConfig.ContextConfig.Compaction.CompactionInterval != nil {
			compactionConfig.CompactionInterval = *agentConfig.ContextConfig.Compaction.CompactionInterval
		}
		if agentConfig.ContextConfig.Compaction.OverlapSize != nil {
			compactionConfig.OverlapSize = *agentConfig.ContextConfig.Compaction.OverlapSize
		}
		if agentConfig.ContextConfig.Compaction.PromptTemplate != "" {
			compactionConfig.SystemPrompt = agentConfig.ContextConfig.Compaction.PromptTemplate
		}

		// Get LLM model for compaction (re-use agent's model)
		log := logr.FromContextOrDiscard(ctx)
		llmModel, err := agent.CreateLLM(ctx, agentConfig.Model, log)
		if err != nil {
			return runner.Config{}, fmt.Errorf("failed to create LLM for compaction: %w", err)
		}

		compactingService, err := compaction.NewCompactingSessionService(
			adkSessionService,
			compactionConfig,
			llmModel,
			log,
		)
		if err != nil {
			return runner.Config{}, fmt.Errorf("failed to create compacting session service: %w", err)
		}
		adkSessionService = compactingService
	}

	// Create memory service if memory is configured
	var memoryService adkmemory.Service
	if agentConfig.Memory != nil {
		// Get Kagent API URL from environment (set by deployment)
		// Defaults to the internal Kubernetes service URL
		apiURL := os.Getenv("KAGENT_API_URL")
		if apiURL == "" {
			apiURL = "http://kagent-controller:8083"
		}

		// Get the agent's model for summarization (re-use the same model)
		var llmModel adkmodel.LLM
		if adkAgent != nil {
			// The agent interface doesn't expose the model directly
			// For now, we'll skip model-based summarization in Go
			// TODO: Extract model from agent or pass separately
			llmModel = nil
		}

		memSvc, err := kagentmemory.New(kagentmemory.Config{
			AgentName:       agentNameFromAppName(appName),
			APIURL:          apiURL,
			TTLDays:         agentConfig.Memory.TTLDays,
			EmbeddingConfig: agentConfig.Memory.Embedding,
			Model:           llmModel,
		})
		if err != nil {
			return runner.Config{}, fmt.Errorf("failed to create memory service: %w", err)
		}
		memoryService = memSvc
	}

	if appName == "" {
		appName = "kagent-app"
	}

	return runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
		MemoryService:  memoryService,
	}, nil
}
