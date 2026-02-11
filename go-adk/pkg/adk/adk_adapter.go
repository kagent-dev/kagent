package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/models"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	adkgemini "google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ModelAdapter wraps a model.LLM and injects MCP tools into each request.
type ModelAdapter struct {
	llm         model.LLM
	logger      logr.Logger
	mcpRegistry *MCPToolRegistry
}

// NewModelAdapter creates an adapter that injects MCP tools into requests and delegates to the given model.LLM.
func NewModelAdapter(llm model.LLM, logger logr.Logger, mcpRegistry *MCPToolRegistry) *ModelAdapter {
	return &ModelAdapter{
		llm:         llm,
		logger:      logger,
		mcpRegistry: mcpRegistry,
	}
}

// Name implements model.LLM
func (m *ModelAdapter) Name() string {
	return m.llm.Name()
}

// GenerateContent implements model.LLM: merge MCP tools into req.Config then delegate to the inner model.
func (m *ModelAdapter) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		reqCopy := cloneLLMRequestWithMCPTools(req, m.mcpRegistry, m.logger)
		m.llm.GenerateContent(ctx, reqCopy, stream)(yield)
	}
}

// openAICompatibleModel creates a model.LLM for OpenAI-compatible endpoints (LiteLLM, Ollama, etc.).
func openAICompatibleModel(baseURL, modelName string, headers map[string]string, logger logr.Logger) (model.LLM, error) {
	return models.NewOpenAICompatibleModelWithLogger(baseURL, modelName, headers, "", logger)
}

// cloneLLMRequestWithMCPTools returns a shallow copy of req with MCP tools merged into Config.Tools.
func cloneLLMRequestWithMCPTools(req *model.LLMRequest, reg *MCPToolRegistry, logger logr.Logger) *model.LLMRequest {
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

func mcpRegistryToGenaiTools(reg *MCPToolRegistry, logger logr.Logger) []*genai.Tool {
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
		if logger.GetSink() != nil {
			var paramNames []string
			if props, ok := params["properties"].(map[string]interface{}); ok {
				for k := range props {
					paramNames = append(paramNames, k)
				}
			}
			schemaJSON := ""
			if len(params) > 0 {
				if b, err := json.Marshal(params); err == nil {
					schemaJSON = string(b)
					if len(schemaJSON) > 1000 {
						schemaJSON = schemaJSON[:1000] + "... (truncated)"
					}
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
}

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig
func CreateGoogleADKAgent(config *core.AgentConfig, logger logr.Logger) (agent.Agent, error) {
	if config == nil {
		return nil, fmt.Errorf("agent config is required")
	}

	if config.Model == nil {
		return nil, fmt.Errorf("model configuration is required")
	}

	mcpRegistry := NewMCPToolRegistry(logger)
	ctx := context.Background()
	fetchHttpTools(ctx, config.HttpTools, mcpRegistry, logger)
	fetchSseTools(ctx, config.SseTools, mcpRegistry, logger)
	adkToolsets := mcpRegistry.GetToolsets()

	// Log final toolset count
	if logger.GetSink() != nil {
		logger.Info("MCP toolsets created", "totalToolsets", len(adkToolsets), "httpToolsCount", len(config.HttpTools), "sseToolsCount", len(config.SseTools), "totalTools", mcpRegistry.GetToolCount())
	}

	// Create model adapter with toolsets
	var modelAdapter model.LLM
	var err error

	// Create model.LLM (OpenAIModel implements it) then wrap with adapter for MCP tool injection
	switch m := config.Model.(type) {
	case *core.OpenAI:
		headers := extractHeaders(m.Headers)
		modelConfig := &models.OpenAIConfig{
			Model:            m.Model,
			BaseUrl:          m.BaseUrl,
			Headers:          headers,
			FrequencyPenalty: m.FrequencyPenalty,
			MaxTokens:        m.MaxTokens,
			N:                m.N,
			PresencePenalty:  m.PresencePenalty,
			ReasoningEffort:  m.ReasoningEffort,
			Seed:             m.Seed,
			Temperature:      m.Temperature,
			Timeout:          m.Timeout,
			TopP:             m.TopP,
		}
		openaiModel, err := models.NewOpenAIModelWithLogger(modelConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI model: %w", err)
		}
		modelAdapter = NewModelAdapter(openaiModel, logger, mcpRegistry)
	case *core.AzureOpenAI:
		headers := extractHeaders(m.Headers)
		modelConfig := &models.AzureOpenAIConfig{
			Model:   m.Model,
			Headers: headers,
			Timeout: nil,
		}
		openaiModel, err := models.NewAzureOpenAIModelWithLogger(modelConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure OpenAI model: %w", err)
		}
		modelAdapter = NewModelAdapter(openaiModel, logger, mcpRegistry)

	// Section 2: Gemini (native API and Vertex AI)
	case *core.Gemini:
		// Native Gemini API (GOOGLE_API_KEY or GEMINI_API_KEY)
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("Gemini model requires GOOGLE_API_KEY or GEMINI_API_KEY environment variable")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "gemini-2.0-flash"
		}
		geminiLLM, err := adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{APIKey: apiKey})
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini model: %w", err)
		}
		modelAdapter = NewModelAdapter(geminiLLM, logger, mcpRegistry)
	case *core.GeminiVertexAI:
		// Vertex AI Gemini (GOOGLE_CLOUD_PROJECT, GOOGLE_CLOUD_LOCATION/REGION, ADC)
		project := os.Getenv("GOOGLE_CLOUD_PROJECT")
		location := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if location == "" {
			location = os.Getenv("GOOGLE_CLOUD_REGION")
		}
		if project == "" || location == "" {
			return nil, fmt.Errorf("GeminiVertexAI requires GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION (or GOOGLE_CLOUD_REGION) environment variables")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "gemini-2.0-flash"
		}
		geminiLLM, err := adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  project,
			Location: location,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini Vertex AI model: %w", err)
		}
		modelAdapter = NewModelAdapter(geminiLLM, logger, mcpRegistry)

	// Section 3: Anthropic (native API), Ollama, GeminiAnthropic via OpenAI-compatible API
	case *core.Anthropic:
		// Native Anthropic API using ANTHROPIC_API_KEY
		modelName := m.Model
		if modelName == "" {
			modelName = "claude-sonnet-4-20250514"
		}
		modelConfig := &models.AnthropicConfig{
			Model:       modelName,
			BaseUrl:     m.BaseUrl, // Optional: can be empty for default API
			Headers:     extractHeaders(m.Headers),
			MaxTokens:   m.MaxTokens,
			Temperature: m.Temperature,
			TopP:        m.TopP,
			TopK:        m.TopK,
			Timeout:     m.Timeout,
		}
		anthropicModel, err := models.NewAnthropicModelWithLogger(modelConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Anthropic model: %w", err)
		}
		modelAdapter = NewModelAdapter(anthropicModel, logger, mcpRegistry)
	case *core.Ollama:
		// Ollama OpenAI-compatible API at http://localhost:11434/v1
		baseURL := "http://localhost:11434/v1"
		modelName := m.Model
		if modelName == "" {
			modelName = "llama3.2"
		}
		openaiModel, err := openAICompatibleModel(baseURL, modelName, extractHeaders(m.Headers), logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Ollama model: %w", err)
		}
		modelAdapter = NewModelAdapter(openaiModel, logger, mcpRegistry)
	case *core.GeminiAnthropic:
		// Claude via OpenAI-compatible endpoint (e.g. LiteLLM); Python uses ADK ClaudeLLM
		baseURL := os.Getenv("LITELLM_BASE_URL")
		if baseURL == "" {
			return nil, fmt.Errorf("GeminiAnthropic (Claude) model requires LITELLM_BASE_URL or configure base_url (e.g. LiteLLM server URL)")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "claude-3-5-sonnet-20241022"
		}
		liteLlmModel := "anthropic/" + modelName
		openaiModel, err := openAICompatibleModel(baseURL, liteLlmModel, extractHeaders(m.Headers), logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create GeminiAnthropic (Claude) model: %w", err)
		}
		modelAdapter = NewModelAdapter(openaiModel, logger, mcpRegistry)

	default:
		return nil, fmt.Errorf("unsupported model type: %s", config.Model.GetType())
	}

	// Create LLM agent config
	agentName := "agent"
	if config.Description != "" {
		// Use description as name if available, otherwise use default
		agentName = "agent" // Default name
	}

	llmAgentConfig := llmagent.Config{
		Name:            agentName,
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
func CreateGoogleADKRunner(config *core.AgentConfig, sessionService core.SessionService, appName string, logger logr.Logger) (*runner.Runner, error) {
	// Create agent
	agent, err := CreateGoogleADKAgent(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Convert our SessionService to Google ADK session.Service
	var adkSessionService session.Service
	if sessionService != nil {
		adkSessionService = NewSessionServiceAdapter(sessionService, logger)
	} else {
		// Use in-memory session service as fallback
		adkSessionService = session.InMemoryService()
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

// extractHeaders extracts headers from a map, returning an empty map if nil
func extractHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return make(map[string]string)
	}
	return headers
}

func fetchHttpTools(ctx context.Context, httpTools []core.HttpMcpServerConfig, mcpRegistry *MCPToolRegistry, logger logr.Logger) {
	if logger.GetSink() != nil {
		logger.Info("Processing HTTP MCP tools", "httpToolsCount", len(httpTools))
	}
	for i, httpTool := range httpTools {
		if logger.GetSink() != nil {
			toolFilterCount := len(httpTool.Tools)
			if toolFilterCount > 0 {
				logger.Info("Adding HTTP MCP tool", "index", i+1, "url", httpTool.Params.Url, "toolFilterCount", toolFilterCount, "tools", httpTool.Tools)
			} else {
				logger.Info("Adding HTTP MCP tool", "index", i+1, "url", httpTool.Params.Url, "toolFilterCount", "all")
			}
		}
		if err := mcpRegistry.FetchToolsFromHttpServer(ctx, httpTool); err != nil {
			if logger.GetSink() != nil {
				logger.Error(err, "Failed to fetch tools from HTTP MCP server", "url", httpTool.Params.Url)
			}
			continue
		}
		if logger.GetSink() != nil {
			logger.Info("Successfully added HTTP MCP toolset", "url", httpTool.Params.Url)
		}
	}
}

func fetchSseTools(ctx context.Context, sseTools []core.SseMcpServerConfig, mcpRegistry *MCPToolRegistry, logger logr.Logger) {
	if logger.GetSink() != nil {
		logger.Info("Processing SSE MCP tools", "sseToolsCount", len(sseTools))
	}
	for i, sseTool := range sseTools {
		if logger.GetSink() != nil {
			toolFilterCount := len(sseTool.Tools)
			if toolFilterCount > 0 {
				logger.Info("Adding SSE MCP tool", "index", i+1, "url", sseTool.Params.Url, "toolFilterCount", toolFilterCount, "tools", sseTool.Tools)
			} else {
				logger.Info("Adding SSE MCP tool", "index", i+1, "url", sseTool.Params.Url, "toolFilterCount", "all")
			}
		}
		if err := mcpRegistry.FetchToolsFromSseServer(ctx, sseTool); err != nil {
			if logger.GetSink() != nil {
				logger.Error(err, "Failed to fetch tools from SSE MCP server", "url", sseTool.Params.Url)
			}
			continue
		}
		if logger.GetSink() != nil {
			logger.Info("Successfully added SSE MCP toolset", "url", sseTool.Params.Url)
		}
	}
}
