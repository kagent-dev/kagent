package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/agent"
	kagentmemory "github.com/kagent-dev/kagent/go/adk/pkg/memory"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/api/adk"
	adkmemory "google.golang.org/adk/memory"
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
	sessionService *session.KAgentSessionService,
	appName string,
	memoryService *kagentmemory.KagentMemoryService,
) (runner.Config, map[string]string, error) {
	var extraTools []adktool.Tool
	if memoryService != nil {
		saveTool, err := kagentmemory.NewSaveMemoryTool(memoryService)
		if err != nil {
			return runner.Config{}, nil, fmt.Errorf("failed to create save_memory tool: %w", err)
		}
		extraTools = append(extraTools, saveTool)
	}

	adkAgent, subagentSessionIDs, err := agent.CreateGoogleADKAgentWithSubagentSessionIDs(ctx, agentConfig, agentNameFromAppName(appName), extraTools...)
	if err != nil {
		return runner.Config{}, nil, fmt.Errorf("failed to create agent: %w", err)
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

	cfg := runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
		MemoryService:  runnerMemory,
	}
	return cfg, subagentSessionIDs, nil
}
