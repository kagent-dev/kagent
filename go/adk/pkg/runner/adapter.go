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
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	"github.com/kagent-dev/kagent/go/api/adk"
	adkmemory "google.golang.org/adk/memory"
	adkplugin "google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	adktool "google.golang.org/adk/tool"
)

func agentNameFromAppName(appName string) string {
	if idx := strings.LastIndex(appName, "__NS__"); idx >= 0 {
		return appName[idx+len("__NS__"):]
	}
	return appName
}

// CreateRunnerConfig builds a runner.Config, subagent session IDs, and
// compaction config for the executor.
func CreateRunnerConfig(
	ctx context.Context,
	agentConfig *adk.AgentConfig,
	sessionService *session.KAgentSessionService,
	appName string,
	memoryService *kagentmemory.KagentMemoryService,
) (runner.Config, map[string]string, *compaction.Config, error) {
	log := logr.FromContextOrDiscard(ctx)

	var extraTools []adktool.Tool
	if memoryService != nil {
		saveTool, err := kagentmemory.NewSaveMemoryTool(memoryService)
		if err != nil {
			return runner.Config{}, nil, nil, fmt.Errorf("failed to create save_memory tool: %w", err)
		}
		extraTools = append(extraTools, saveTool)
	}

	stsPlugin, err := buildTokenPropagationPlugin(ctx, log)
	if err != nil {
		return runner.Config{}, nil, nil, err
	}

	adkAgent, subagentSessionIDs, err := agent.CreateGoogleADKAgentWithSubagentSessionIDs(ctx, agentConfig, agentNameFromAppName(appName), stsPlugin, extraTools...)
	if err != nil {
		return runner.Config{}, nil, nil, fmt.Errorf("failed to create agent: %w", err)
	}

	var adkSessionService adksession.Service
	if sessionService != nil {
		adkSessionService = sessionService
	} else {
		adkSessionService = adksession.InMemoryService()
	}

	if appName == "" {
		appName = "kagent-app"
	}

	var runnerMemory adkmemory.Service
	if memoryService != nil {
		runnerMemory = memoryService
	}

	var adkPlugins []*adkplugin.Plugin
	if stsPlugin != nil {
		p, err := stsPlugin.ADKPlugin()
		if err != nil {
			return runner.Config{}, nil, nil, fmt.Errorf("failed to create STS ADK plugin: %w", err)
		}
		if p != nil {
			adkPlugins = append(adkPlugins, p)
		}
	}

	cfg := runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
		MemoryService:  runnerMemory,
		PluginConfig: runner.PluginConfig{
			Plugins: adkPlugins,
		},
	}

	compactionCfg, err := buildCompactionConfig(ctx, agentConfig, log)
	if err != nil {
		return runner.Config{}, nil, nil, fmt.Errorf("failed to build compaction config: %w", err)
	}

	return cfg, subagentSessionIDs, compactionCfg, nil
}

// buildCompactionConfig builds a *compaction.Config from the agent config,
// wiring the summarizer LLM if one is configured.
func buildCompactionConfig(ctx context.Context, agentConfig *adk.AgentConfig, log logr.Logger) (*compaction.Config, error) {
	cfg, err := compaction.FromAgentConfig(agentConfig)
	if err != nil || cfg == nil {
		return cfg, err
	}

	summarizerModelName := compaction.SummarizerModelName(agentConfig)
	if summarizerModelName != "" {
		// SummarizerModel is configured as a full Model spec in the agent config.
		if agentConfig.ContextConfig != nil && agentConfig.ContextConfig.Compaction != nil &&
			agentConfig.ContextConfig.Compaction.SummarizerModel != nil {
			summarizerLLM, err := agent.CreateLLM(ctx, agentConfig.ContextConfig.Compaction.SummarizerModel, log)
			if err != nil {
				return nil, fmt.Errorf("failed to create summarizer LLM: %w", err)
			}
			cfg.SummarizerLLM = summarizerLLM
		}
	} else if agentConfig.Model != nil {
		// Fall back to the agent's own model for summarization.
		summarizerLLM, err := agent.CreateLLM(ctx, agentConfig.Model, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create summarizer LLM from agent model: %w", err)
		}
		cfg.SummarizerLLM = summarizerLLM
	}

	return cfg, nil
}

func buildTokenPropagationPlugin(ctx context.Context, log logr.Logger) (*sts.TokenPropagationPlugin, error) {
	propagateToken := strings.EqualFold(strings.TrimSpace(os.Getenv("KAGENT_PROPAGATE_TOKEN")), "true")
	stsWellKnownURI := strings.TrimSpace(os.Getenv("STS_WELL_KNOWN_URI"))
	if !propagateToken && stsWellKnownURI == "" {
		return nil, nil
	}

	// Propagate-only mode: keep parity with Python by enabling plugin without STS exchange.
	if stsWellKnownURI == "" {
		log.Info("Enabling token propagation plugin without STS exchange")
		return sts.NewTokenPropagationPlugin(nil, log), nil
	}
	defaultSTSConfig := sts.DefaultSTSConfig(stsWellKnownURI)

	integration, err := sts.NewSTSIntegration(
		stsWellKnownURI,
		"",
		nil, // fetchActorToken
		nil, // getSubjectToken
		defaultSTSConfig.Timeout,
		*defaultSTSConfig.VerifySSL,
		defaultSTSConfig.UseIssuerHost,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize STS integration: %w", err)
	}

	log.Info("Enabling STS token propagation plugin", "wellKnownURI", stsWellKnownURI)
	return sts.NewTokenPropagationPlugin(integration, log), nil
}
