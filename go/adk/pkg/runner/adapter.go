package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

	"log/slog"

	"github.com/kagent-dev/kagent/go/adk/pkg/agent"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	kagentmemory "github.com/kagent-dev/kagent/go/adk/pkg/memory"
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/internal/logging"
	adkmemory "google.golang.org/adk/v2/memory"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	adktool "google.golang.org/adk/v2/tool"
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
	controllerClient *controllerclient.Client,
) (runner.Config, error) {
	log := logging.FromContext(ctx)

	var extraTools []adktool.Tool
	if memoryService != nil {
		saveTool, err := kagentmemory.NewSaveMemoryTool(memoryService)
		if err != nil {
			return runner.Config{}, fmt.Errorf("failed to create save_memory tool: %w", err)
		}
		extraTools = append(extraTools, saveTool)
	}

	stsPlugin, err := buildTokenPropagationPlugin(ctx, log)
	if err != nil {
		return runner.Config{}, err
	}

	adkAgent, err := agent.CreateGoogleADKAgent(ctx, agentConfig, agentNameFromAppName(appName), stsPlugin, extraTools...)
	if err != nil {
		return runner.Config{}, fmt.Errorf("failed to create agent: %w", err)
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
			return runner.Config{}, fmt.Errorf("failed to create STS ADK plugin: %w", err)
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

	return cfg, nil
}

func buildTokenPropagationPlugin(ctx context.Context, log *slog.Logger) (*sts.TokenPropagationPlugin, error) {
	propagateToken := strings.EqualFold(strings.TrimSpace(os.Getenv("KAGENT_PROPAGATE_TOKEN")), "true")
	stsWellKnownURI := strings.TrimSpace(os.Getenv("STS_WELL_KNOWN_URI"))
	if !propagateToken && stsWellKnownURI == "" {
		return nil, nil
	}

	// Propagate-only mode: keep parity with Python by enabling plugin without STS exchange.
	if stsWellKnownURI == "" {
		log.InfoContext(ctx, "enabling token propagation plugin without STS exchange")
		return sts.NewTokenPropagationPlugin(nil, log, nil, nil), nil
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

	// RFC 8707 resource / RFC 8693 audience scope the exchanged token to a
	// backend. Both are repeatable; empty values are omitted from the request.
	resource := splitCSV(os.Getenv("KAGENT_STS_RESOURCE"))
	audience := splitCSV(os.Getenv("KAGENT_STS_AUDIENCE"))

	log.InfoContext(ctx, "enabling STS token propagation plugin", "well_known_uri", stsWellKnownURI)
	return sts.NewTokenPropagationPlugin(integration, log, resource, audience), nil
}

// splitCSV parses a comma-separated value into trimmed, non-empty entries,
// returning nil when none are present so the exchange target stays unset.
func splitCSV(v string) []string {
	var out []string
	for p := range strings.SplitSeq(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
