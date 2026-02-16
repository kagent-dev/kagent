package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/mcp"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/models"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/types"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ModelAdapter wraps a adkmodel.LLM and injects MCP tools into each request.
type ModelAdapter struct {
	llm         adkmodel.LLM
	logger      logr.Logger
	mcpRegistry *mcp.MCPToolRegistry
}

// NewModelAdapter creates an adapter that injects MCP tools into requests and delegates to the given adkmodel.LLM.
func NewModelAdapter(llm adkmodel.LLM, logger logr.Logger, mcpRegistry *mcp.MCPToolRegistry) *ModelAdapter {
	return &ModelAdapter{
		llm:         llm,
		logger:      logger,
		mcpRegistry: mcpRegistry,
	}
}

// Name implements adkmodel.LLM
func (m *ModelAdapter) Name() string {
	return m.llm.Name()
}

// GenerateContent implements adkmodel.LLM: merge MCP tools into req.Config then delegate to the inner adkmodel.
func (m *ModelAdapter) GenerateContent(ctx context.Context, req *adkmodel.LLMRequest, stream bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		reqCopy := cloneLLMRequestWithMCPTools(req, m.mcpRegistry, m.logger)
		m.llm.GenerateContent(ctx, reqCopy, stream)(yield)
	}
}

// cloneLLMRequestWithMCPTools returns a shallow copy of req with MCP tools merged into Config.Tools.
func cloneLLMRequestWithMCPTools(req *adkmodel.LLMRequest, reg *mcp.MCPToolRegistry, logger logr.Logger) *adkmodel.LLMRequest {
	if req == nil {
		return nil
	}
	out := *req
	if reg != nil && reg.GetToolCount() > 0 {
		mcpTools := mcpRegistryToGenaiTools(reg, logger)
		if len(mcpTools) > 0 {
			if out.Config == nil {
				out.Config = &genai.GenerateContentConfig{}
			}
			configCopy := *out.Config
			configCopy.Tools = append(append([]*genai.Tool(nil), configCopy.Tools...), mcpTools...)
			out.Config = &configCopy
		}
	}
	return &out
}

func mcpRegistryToGenaiTools(reg *mcp.MCPToolRegistry, logger logr.Logger) []*genai.Tool {
	decls := reg.GetToolsAsFunctionDeclarations()
	if len(decls) == 0 {
		return nil
	}
	ensureToolSchema(decls, logger)
	genaiDecls := make([]*genai.FunctionDeclaration, 0, len(decls))
	for i := range decls {
		params := decls[i].Parameters
		if params == nil {
			params = make(map[string]interface{})
		}
		genaiDecls = append(genaiDecls, &genai.FunctionDeclaration{
			Name:                 decls[i].Name,
			Description:          decls[i].Description,
			ParametersJsonSchema: params,
		})
	}
	return []*genai.Tool{{FunctionDeclarations: genaiDecls}}
}

// ensureToolSchema ensures each function declaration has OpenAI-required schema fields.
func ensureToolSchema(funcDecls []models.FunctionDeclaration, logger logr.Logger) {
	for i := range funcDecls {
		params := funcDecls[i].Parameters
		if params == nil {
			params = make(map[string]interface{})
			funcDecls[i].Parameters = params
		}
		if params["type"] == nil {
			params["type"] = "object"
		}
		if _, ok := params["properties"].(map[string]interface{}); !ok {
			params["properties"] = make(map[string]interface{})
		}
		if _, ok := params["required"].([]interface{}); !ok {
			params["required"] = []interface{}{}
		}
	}
	if logger.GetSink() != nil {
		logToolDeclarations(funcDecls, logger)
	}
}

// logToolDeclarations logs details of each function declaration at V(1).
func logToolDeclarations(funcDecls []models.FunctionDeclaration, logger logr.Logger) {
	for i := range funcDecls {
		params := funcDecls[i].Parameters
		var paramNames []string
		if props, ok := params["properties"].(map[string]interface{}); ok {
			for k := range props {
				paramNames = append(paramNames, k)
			}
		}
		schemaJSON := ""
		if b, err := json.Marshal(params); err == nil {
			schemaJSON = string(b)
			if len(schemaJSON) > 1000 {
				schemaJSON = schemaJSON[:1000] + "... (truncated)"
			}
		}
		logger.V(1).Info("Using tool from MCPToolRegistry",
			"functionName", funcDecls[i].Name,
			"description", funcDecls[i].Description,
			"parameterNames", paramNames,
			"parameterCount", len(paramNames),
			"schema", schemaJSON)
	}
}

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig
func createGoogleADKAgent(config *types.AgentConfig, logger logr.Logger) (agent.Agent, error) {
	return createGoogleADKAgentWithFactory(config, models.NewDefaultLLMFactory(), logger)
}

// CreateGoogleADKAgentWithFactory creates a Google ADK agent using a custom LLM factory.
func createGoogleADKAgentWithFactory(config *types.AgentConfig, llmFactory models.LLMFactory, logger logr.Logger) (agent.Agent, error) {
	if config == nil {
		return nil, fmt.Errorf("agent config is required")
	}

	if config.Model == nil {
		return nil, fmt.Errorf("model configuration is required")
	}

	mcpRegistry := mcp.NewMCPToolRegistry(logger)
	ctx := context.Background()
	fetchMCPTools(ctx, mcpRegistry, config.HttpTools, config.SseTools, logger)
	adkToolsets := mcpRegistry.GetToolsets()

	// Log final toolset count
	if logger.GetSink() != nil {
		logger.Info("MCP toolsets created", "totalToolsets", len(adkToolsets), "httpToolsCount", len(config.HttpTools), "sseToolsCount", len(config.SseTools), "totalTools", mcpRegistry.GetToolCount())
	}

	// Create adkmodel.LLM using factory then wrap with adapter for MCP tool injection
	llm, err := llmFactory.CreateLLM(ctx, config.Model, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM: %w", err)
	}
	modelAdapter := NewModelAdapter(llm, logger, mcpRegistry)

	llmAgentConfig := llmagent.Config{
		Name:            "agent",
		Description:     config.Description,
		Instruction:     config.Instruction,
		Model:           modelAdapter,
		IncludeContents: llmagent.IncludeContentsDefault, // Include conversation history
		Toolsets:        adkToolsets,
	}

	// Log agent configuration for debugging
	if logger.GetSink() != nil {
		logger.Info("Creating Google ADK LLM agent",
			"name", llmAgentConfig.Name,
			"hasDescription", llmAgentConfig.Description != "",
			"hasInstruction", llmAgentConfig.Instruction != "",
			"toolsetsCount", len(llmAgentConfig.Toolsets))
	}

	// Create the LLM agent
	llmAgent, err := llmagent.New(llmAgentConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	if logger.GetSink() != nil {
		logger.Info("Successfully created Google ADK LLM agent", "toolsetsCount", len(llmAgentConfig.Toolsets))
	}

	return llmAgent, nil
}

// CreateGoogleADKRunner creates a Google ADK Runner from AgentConfig.
// appName must match the executor's AppName so session lookup returns the same session with prior events
// (Python: runner.app_name; ensures LLM receives full context on resume after user response).
func CreateGoogleADKRunner(config *types.AgentConfig, sessionService session.SessionService, appName string, logger logr.Logger) (*runner.Runner, error) {
	// Create agent
	agent, err := createGoogleADKAgent(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Convert our SessionService to Google ADK session.Service
	var adkSessionService adksession.Service
	if sessionService != nil {
		adkSessionService = NewSessionServiceAdapter(sessionService, logger)
	} else {
		// Use in-memory session service as fallback
		adkSessionService = adksession.InMemoryService()
	}

	// Use provided app name so runner's session lookup matches executor's (same session = full LLM context on resume)
	if appName == "" {
		appName = "kagent-app"
	}

	runnerConfig := runner.Config{
		AppName:        appName,
		Agent:          agent,
		SessionService: adkSessionService,
		// ArtifactService and MemoryService are optional
	}

	// Create runner
	adkRunner, err := runner.New(runnerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	return adkRunner, nil
}

// mcpServerSource abstracts HTTP and SSE MCP server configs for fetchMCPTools.
type mcpServerSource interface {
	url() string
	toolFilter() []string
	fetch(ctx context.Context, reg *mcp.MCPToolRegistry) error
}

type httpSource struct{ cfg types.HttpMcpServerConfig }

func (h httpSource) url() string          { return h.cfg.Params.Url }
func (h httpSource) toolFilter() []string { return h.cfg.Tools }
func (h httpSource) fetch(ctx context.Context, reg *mcp.MCPToolRegistry) error {
	return reg.FetchToolsFromHttpServer(ctx, h.cfg)
}

type sseSource struct{ cfg types.SseMcpServerConfig }

func (s sseSource) url() string          { return s.cfg.Params.Url }
func (s sseSource) toolFilter() []string { return s.cfg.Tools }
func (s sseSource) fetch(ctx context.Context, reg *mcp.MCPToolRegistry) error {
	return reg.FetchToolsFromSseServer(ctx, s.cfg)
}

// fetchMCPTools fetches tools from both HTTP and SSE MCP servers into the registry.
func fetchMCPTools(ctx context.Context, reg *mcp.MCPToolRegistry, httpTools []types.HttpMcpServerConfig, sseTools []types.SseMcpServerConfig, logger logr.Logger) {
	sources := make([]mcpServerSource, 0, len(httpTools)+len(sseTools))
	for _, h := range httpTools {
		sources = append(sources, httpSource{h})
	}
	for _, s := range sseTools {
		sources = append(sources, sseSource{s})
	}

	if logger.GetSink() != nil {
		logger.Info("Processing MCP tools", "httpCount", len(httpTools), "sseCount", len(sseTools))
	}

	for i, src := range sources {
		if logger.GetSink() != nil {
			filterCount := len(src.toolFilter())
			if filterCount > 0 {
				logger.Info("Adding MCP tool", "index", i+1, "url", src.url(), "toolFilterCount", filterCount, "tools", src.toolFilter())
			} else {
				logger.Info("Adding MCP tool", "index", i+1, "url", src.url(), "toolFilterCount", "all")
			}
		}
		if err := src.fetch(ctx, reg); err != nil {
			if logger.GetSink() != nil {
				logger.Error(err, "Failed to fetch tools from MCP server", "url", src.url())
			}
			continue
		}
		if logger.GetSink() != nil {
			logger.Info("Successfully added MCP toolset", "url", src.url())
		}
	}
}
