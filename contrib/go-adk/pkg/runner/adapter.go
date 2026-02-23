package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go-adk/pkg/agent"
	"github.com/kagent-dev/kagent/go-adk/pkg/config"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

func agentNameFromAppName(appName string) string {
	if idx := strings.LastIndex(appName, "__NS__"); idx >= 0 {
		return appName[idx+len("__NS__"):]
	}
	return appName
}

// CreateRunnerConfig creates a runner.Config suitable for use with adka2a.Executor.
func CreateRunnerConfig(ctx context.Context, agentConfig *config.AgentConfig, sessionService session.SessionService, toolsets []tool.Toolset, appName string) (runner.Config, error) {
	adkAgent, err := agent.CreateGoogleADKAgent(ctx, agentConfig, toolsets, agentNameFromAppName(appName))
	if err != nil {
		return runner.Config{}, fmt.Errorf("failed to create agent: %w", err)
	}

	var adkSessionService adksession.Service
	if sessionService != nil {
		adkSessionService = session.NewSessionServiceAdapter(sessionService)
	} else {
		adkSessionService = adksession.InMemoryService()
	}

	if appName == "" {
		appName = "kagent-app"
	}

	return runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
	}, nil
}

