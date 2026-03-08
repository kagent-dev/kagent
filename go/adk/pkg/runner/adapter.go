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

// CreateRunnerConfig creates a runner.Config suitable for use with adka2a.Executor.
// memoryService is optional; pass nil when memory is not configured.
func CreateRunnerConfig(ctx context.Context, agentConfig *adk.AgentConfig, sessionService session.SessionService, appName string, memoryService *kagentmemory.KagentMemoryService) (runner.Config, error) {
	// If a memory service is provided, create the save_memory tool so the agent
	// can explicitly save content. The load_memory tool is provided by the
	// upstream Google ADK.
	var extraTools []adktool.Tool
	if memoryService != nil {
		extraTools = append(extraTools, kagentmemory.NewSaveMemoryTool(memoryService))
	}

	adkAgent, err := agent.CreateGoogleADKAgent(ctx, agentConfig, agentNameFromAppName(appName), extraTools...)
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

	// The runner's MemoryService handles automatic session-level memory
	// (AddSession after each turn). The save_memory tool handles explicit saves.
	var runnerMemory adkmemory.Service
	if memoryService != nil {
		runnerMemory = memoryService
	}

	return runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
		MemoryService:  runnerMemory,
	}, nil
}
