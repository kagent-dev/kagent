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

type SubagentSessionProvider interface {
	SubagentSessionID() string
}

// ExtractSubagentSessionIDs returns a map of toolset-name -> session-ID
// for every toolset that implements SubagentSessionProvider.
func ExtractSubagentSessionIDs(toolsets []adktool.Toolset) map[string]string {
	result := make(map[string]string)
	for _, ts := range toolsets {
		if provider, ok := ts.(SubagentSessionProvider); ok {
			if id := provider.SubagentSessionID(); id != "" {
				result[ts.Name()] = id
			}
		}
	}
	return result
}

// CreateRunnerConfig builds a runner.Config and extracts subagent session IDs
// from the agent's toolsets.
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

	adkAgent, toolsets, err := agent.CreateGoogleADKAgentAndToolsets(ctx, agentConfig, agentNameFromAppName(appName), extraTools...)
	if err != nil {
		return runner.Config{}, nil, fmt.Errorf("failed to create agent: %w", err)
	}

	subagentSessionIDs := ExtractSubagentSessionIDs(toolsets)

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
