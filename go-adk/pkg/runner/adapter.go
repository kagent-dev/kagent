package runner

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go-adk/pkg/agent"
	"github.com/kagent-dev/kagent/go-adk/pkg/config"
	"github.com/kagent-dev/kagent/go-adk/pkg/session"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

// CreateGoogleADKRunner creates a Google ADK Runner from AgentConfig.
// appName must match the executor's AppName so session lookup returns the same session with prior events.
func CreateGoogleADKRunner(ctx context.Context, agentConfig *config.AgentConfig, sessionService session.SessionService, toolsets []tool.Toolset, appName string) (*runner.Runner, error) {
	adkAgent, err := agent.CreateGoogleADKAgent(ctx, agentConfig, toolsets)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
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

	runnerConfig := runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
	}

	adkRunner, err := runner.New(runnerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	return adkRunner, nil
}
