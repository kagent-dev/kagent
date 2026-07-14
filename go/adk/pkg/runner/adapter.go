package runner

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/agent"
	kagentmemory "github.com/kagent-dev/kagent/go/adk/pkg/memory"
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	"github.com/kagent-dev/kagent/go/adk/pkg/tools"
	"github.com/kagent-dev/kagent/go/api/adk"
	adkmemory "google.golang.org/adk/memory"
	adkplugin "google.golang.org/adk/plugin"
	"google.golang.org/adk/plugin/loggingplugin"
	"google.golang.org/adk/plugin/retryandreflect"
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

// CreateRunnerConfig builds a runner.Config and subagent session IDs for A2A
// stamping (from remote agent wiring in the agent builder).
func CreateRunnerConfig(
	ctx context.Context,
	agentConfig *adk.AgentConfig,
	sessionService adksession.Service,
	appName string,
	memoryService *kagentmemory.KagentMemoryService,
	kagentURL string,
	httpClient *http.Client,
) (runner.Config, map[string]string, error) {
	log := logr.FromContextOrDiscard(ctx)

	var extraTools []adktool.Tool
	if memoryService != nil {
		saveTool, err := kagentmemory.NewSaveMemoryTool(memoryService)
		if err != nil {
			return runner.Config{}, nil, fmt.Errorf("failed to create save_memory tool: %w", err)
		}
		extraTools = append(extraTools, saveTool)
	}

	if agentConfig.ShareTools != nil && *agentConfig.ShareTools && kagentURL != "" && httpClient != nil {
		createTool, err := tools.NewCreateShareLinkTool(httpClient, kagentURL, appName)
		if err != nil {
			return runner.Config{}, nil, fmt.Errorf("failed to create create_share_link tool: %w", err)
		}
		listTool, err := tools.NewListShareLinksTool(httpClient, kagentURL, appName)
		if err != nil {
			return runner.Config{}, nil, fmt.Errorf("failed to create list_share_links tool: %w", err)
		}
		deleteTool, err := tools.NewDeleteShareLinkTool(httpClient, kagentURL, appName)
		if err != nil {
			return runner.Config{}, nil, fmt.Errorf("failed to create delete_share_link tool: %w", err)
		}
		extraTools = append(extraTools, createTool, listTool, deleteTool)
		log.Info("Share link tools enabled")
	}

	stsPlugin, err := buildTokenPropagationPlugin(ctx, log)
	if err != nil {
		return runner.Config{}, nil, err
	}

	adkAgent, subagentSessionIDs, err := agent.CreateGoogleADKAgentWithSubagentSessionIDs(ctx, agentConfig, agentNameFromAppName(appName), stsPlugin, extraTools...)
	if err != nil {
		return runner.Config{}, nil, fmt.Errorf("failed to create agent: %w", err)
	}

	adkSessionService := sessionService
	if adkSessionService == nil {
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
			return runner.Config{}, nil, fmt.Errorf("failed to create STS ADK plugin: %w", err)
		}
		if p != nil {
			adkPlugins = append(adkPlugins, p)
		}
	}

	reliabilityPlugins, err := buildReliabilityPlugins(agentConfig.Reliability, log)
	if err != nil {
		return runner.Config{}, nil, err
	}
	adkPlugins = append(adkPlugins, reliabilityPlugins...)

	cfg := runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
		MemoryService:  runnerMemory,
		PluginConfig: runner.PluginConfig{
			Plugins: adkPlugins,
		},
	}

	return cfg, subagentSessionIDs, nil
}

// buildReliabilityPlugins translates the agent reliability configuration into
// ADK plugins: debug logging, tool retry (reflect-and-retry), and a max LLM
// calls limit.
func buildReliabilityPlugins(r *adk.ReliabilityConfig, log logr.Logger) ([]*adkplugin.Plugin, error) {
	if r == nil {
		return nil, nil
	}

	var plugins []*adkplugin.Plugin

	if r.DebugLogging != nil && *r.DebugLogging {
		p, err := loggingplugin.New("kagent_debug_logging")
		if err != nil {
			return nil, fmt.Errorf("failed to create debug logging plugin: %w", err)
		}
		plugins = append(plugins, p)
		log.Info("Debug logging enabled for agent")
	}

	if r.ToolRetries != nil && *r.ToolRetries > 0 {
		p, err := retryandreflect.New(
			retryandreflect.WithMaxRetries(*r.ToolRetries),
			retryandreflect.WithErrorIfRetryExceeded(false),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create tool retry plugin: %w", err)
		}
		plugins = append(plugins, p)
		log.Info("Tool retry enabled for agent", "toolRetries", *r.ToolRetries)
	}

	if r.MaxLLMCalls != nil && *r.MaxLLMCalls > 0 {
		p, err := newMaxLLMCallsPlugin(*r.MaxLLMCalls)
		if err != nil {
			return nil, fmt.Errorf("failed to create max LLM calls plugin: %w", err)
		}
		plugins = append(plugins, p)
		log.Info("Max LLM calls limit enabled for agent", "maxLLMCalls", *r.MaxLLMCalls)
	}

	return plugins, nil
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
