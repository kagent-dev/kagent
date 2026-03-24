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
// Also returns subagentSessionIDs: a map of tool name → pre-generated context_id
// for each KAgentRemoteA2ATool, to be forwarded to NewExecutorConfig so the
// executor can stamp function_call DataParts for UI live-polling.
// sessionService implements adksession.Service directly; pass nil for in-memory.
// memoryService is optional; pass nil when memory is not configured.
func CreateRunnerConfig(
	ctx context.Context,
	agentConfig *adk.AgentConfig,
	sessionService *session.KAgentSessionService,
	appName string,
	memoryService *kagentmemory.KagentMemoryService,
) (runner.Config, map[string]string, error) {
	var extraTools []adktool.Tool
	if memoryService != nil {
		extraTools = append(extraTools, kagentmemory.NewSaveMemoryTool(memoryService))
	}

	adkAgent, subagentSessionIDs, err := agent.CreateGoogleADKAgent(ctx, agentConfig, agentNameFromAppName(appName), extraTools...)
	if err != nil {
		return runner.Config{}, nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// KAgentSessionService implements adksession.Service directly.
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

	return runner.Config{
		AppName:        appName,
		Agent:          adkAgent,
		SessionService: adkSessionService,
		MemoryService:  runnerMemory,
	}, subagentSessionIDs, nil
}
