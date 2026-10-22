package config

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/agentplugins"
)

// DefaultSkillsRoot is where the ADK installs root-agent skills.
const DefaultSkillsRoot = "/skills"

// AgentPluginPaths contains the ADK runtime destinations for Agent Plugin
// packages, selected skills, and mutable MCP server data.
type AgentPluginPaths struct {
	Packages string
	Skills   string
	Data     string
}

// MaterializeAgentPlugins materializes plugins independently for each agent
// and adds their skills and MCP servers to the ADK runtime configuration.
// Downloaded standalone skills are fetched best-effort at startup; the
// returned function installs any still missing and must run before each task.
func MaterializeAgentPlugins(ctx context.Context, agentConfig *configv1.ADKAgentConfig, paths AgentPluginPaths) (func(context.Context) error, error) {
	var materializations []agentplugins.Materialization
	var visit func(*configv1.ADKAgentConfig, AgentPluginPaths) error
	visit = func(agentConfig *configv1.ADKAgentConfig, paths AgentPluginPaths) error {
		if agentConfig.AgentPlugins != nil {
			materialization, err := agentplugins.Materialize(ctx, PluginResources(agentConfig.AgentPlugins), agentplugins.Paths{
				Packages: paths.Packages,
				Skills:   paths.Skills,
			})
			if err != nil {
				return fmt.Errorf("materialize agent plugins: %w", err)
			}
			if len(materialization.ClaudeFormatPluginRoots()) > 0 {
				return fmt.Errorf("plugin is Claude-format; only the Agent Plugins format (plugin.json at the plugin root) is supported here")
			}
			mcpConfig, err := agentplugins.LoadMCP(ctx, materialization, paths.Data)
			if err != nil {
				return fmt.Errorf("load agent plugin MCP configuration: %w", err)
			}
			addMCPConfig(agentConfig, mcpConfig)
			agentConfig.SkillsDirectory = materialization.SkillsDirectory
			materializations = append(materializations, materialization)
		}
		for i, child := range agentConfig.SubAgents {
			childRoot := filepath.Join("subagents", fmt.Sprintf("%d", i))
			if err := visit(child, AgentPluginPaths{
				Packages: filepath.Join(paths.Packages, childRoot),
				Skills:   filepath.Join(paths.Skills, childRoot),
				Data:     filepath.Join(paths.Data, childRoot),
			}); err != nil {
				return fmt.Errorf("materialize sub-agent %q: %w", child.Name, err)
			}
		}
		return nil
	}
	if err := visit(agentConfig, paths); err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		var errs []error
		for _, materialization := range materializations {
			errs = append(errs, materialization.EnsureSkills(ctx))
		}
		return errors.Join(errs...)
	}, nil
}

func addMCPConfig(agentConfig *configv1.ADKAgentConfig, mcpConfig agentplugins.MCPConfig) {
	for _, server := range mcpConfig.StreamableHTTP {
		agentConfig.HttpTools = append(agentConfig.HttpTools, &configv1.ADKHTTPMCPServerConfig{
			Params: &configv1.ADKHTTPConnectionParams{Url: server.URL, Headers: server.Headers},
		})
	}
	for _, server := range mcpConfig.SSE {
		agentConfig.SseTools = append(agentConfig.SseTools, &configv1.ADKSSEMCPServerConfig{
			Params: &configv1.ADKSSEConnectionParams{Url: server.URL, Headers: server.Headers},
		})
	}
	for _, server := range mcpConfig.Stdio {
		agentConfig.StdioTools = append(agentConfig.StdioTools, &configv1.ADKStdioMCPServerConfig{
			Command: server.Command,
			Args:    server.Args,
			Env:     server.Env,
			Dir:     server.Dir,
		})
	}
}
