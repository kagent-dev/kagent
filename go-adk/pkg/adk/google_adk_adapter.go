package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"iter"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/models"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ModelAdapter wraps our models.BaseLLM to implement Google ADK's model.LLM interface
type ModelAdapter struct {
	baseLLM     models.BaseLLM
	logger      logr.Logger
	mcpRegistry *MCPToolRegistry // MCP tool registry that has tool schemas
}

// NewModelAdapter creates a new adapter that wraps models.BaseLLM as model.LLM
func NewModelAdapter(baseLLM models.BaseLLM, logger logr.Logger, mcpRegistry *MCPToolRegistry) *ModelAdapter {
	return &ModelAdapter{
		baseLLM:     baseLLM,
		logger:      logger,
		mcpRegistry: mcpRegistry,
	}
}

// Name implements model.LLM interface
func (m *ModelAdapter) Name() string {
	// Return a descriptive name based on the model type
	return "adapter-model"
}

// GenerateContent implements model.LLM interface
func (m *ModelAdapter) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Convert Google ADK LLMRequest to our internal LLMRequest
		internalReq, err := m.convertLLMRequest(req)
		if err != nil {
			yield(nil, fmt.Errorf("failed to convert LLM request: %w", err))
			return
		}

		// Call our base LLM
		responseChan, err := m.baseLLM.GenerateContent(ctx, internalReq, stream)
		if err != nil {
			yield(nil, err)
			return
		}

		// Convert our responses to Google ADK responses
		for resp := range responseChan {
			adkResp := m.convertLLMResponse(resp)
			if !yield(adkResp, nil) {
				return
			}
		}
	}
}

// convertLLMRequest converts Google ADK LLMRequest to our internal LLMRequest.
// The ADK builds req.Contents from ctx.Session().Events() (user + model + tool events).
// We copy all contents and system instruction unchanged so the second LLM call (after
// MCP tool response) gets full history: [user message, model with function_call, tool with function_response].
func (m *ModelAdapter) convertLLMRequest(req *model.LLMRequest) (*models.LLMRequest, error) {
	// Log request shape for debugging (systemInstruction + contents/roles same on every call)
	if m.logger.GetSink() != nil {
		roles := make([]string, 0, len(req.Contents))
		for _, c := range req.Contents {
			roles = append(roles, string(c.Role))
		}
		hasSys := req.Config != nil && req.Config.SystemInstruction != nil && len(req.Config.SystemInstruction.Parts) > 0
		m.logger.V(1).Info("convertLLMRequest: ADK request",
			"contentsCount", len(req.Contents),
			"roles", roles,
			"hasSystemInstruction", hasSys)
	}

	// Convert contents. System prompt belongs only in Config.SystemInstruction (system role);
	// contents must be conversation only (user/model). User input must be user role.
	contents := make([]models.Content, 0, len(req.Contents))
	for _, content := range req.Contents {
		roleStr := strings.TrimSpace(string(content.Role))
		// Skip system-role content; system prompt is only in req.Config.SystemInstruction
		if roleStr == "system" {
			continue
		}
		parts := make([]models.Part, 0, len(content.Parts))
		for _, part := range content.Parts {
			internalPart := models.Part{}
			if part.Text != "" {
				text := part.Text
				internalPart.Text = &text
			}
			// Convert function calls
			if part.FunctionCall != nil {
				internalPart.FunctionCall = &models.FunctionCall{
					ID:   part.FunctionCall.ID,
					Name: part.FunctionCall.Name,
					Args: part.FunctionCall.Args,
				}
			}
			// Convert function responses
			if part.FunctionResponse != nil {
				// Convert response map to interface{}
				var responseValue interface{} = part.FunctionResponse.Response
				internalPart.FunctionResponse = &models.FunctionResponse{
					ID:       part.FunctionResponse.ID,
					Name:     part.FunctionResponse.Name,
					Response: responseValue,
				}
			}
			// Convert inline data if present
			if part.InlineData != nil {
				internalPart.InlineData = &models.InlineData{
					MimeType: part.InlineData.MIMEType,
					Data:     part.InlineData.Data,
				}
			}
			// Convert file data if present
			if part.FileData != nil {
				internalPart.FileData = &models.FileData{
					FileURI:  part.FileData.FileURI,
					MimeType: part.FileData.MIMEType,
				}
			}
			parts = append(parts, internalPart)
		}
		contents = append(contents, models.Content{
			Role:  string(content.Role),
			Parts: parts,
		})
	}

	// Copy system instruction from ADK request into our internal request (Python-like).
	// Python: system_instruction comes only from llm_request.config.system_instruction;
	// contents come only from session events (user, model, tool). We do the same: use
	// only req.Config.SystemInstruction for system message; pass req.Contents as-is.
	var systemInstruction interface{}
	if req.Config != nil && req.Config.SystemInstruction != nil {
		var textParts []string
		for _, part := range req.Config.SystemInstruction.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
		}
		if len(textParts) > 0 {
			systemInstruction = strings.Join(textParts, "\n")
		}
	}

	// Extract tools from MCPToolRegistry (schemas populated when toolsets are created)
	tools := m.extractToolsFromRegistry()

	internalReq := &models.LLMRequest{
		Model:    req.Model,
		Contents: contents,
		Config: &models.LLMRequestConfig{
			SystemInstruction: systemInstruction,
			Tools:             tools,
		},
	}

	return internalReq, nil
}

// extractToolsFromRegistry returns tools from MCPToolRegistry with schemas normalized for OpenAI.
func (m *ModelAdapter) extractToolsFromRegistry() []models.Tool {
	if m.mcpRegistry == nil || m.mcpRegistry.GetToolCount() == 0 {
		if m.logger.GetSink() != nil {
			if m.mcpRegistry == nil {
				m.logger.Info("MCPToolRegistry is nil, no tools will be passed to LLM")
			} else {
				m.logger.Info("MCPToolRegistry has no tools, no tools will be passed to LLM")
			}
		}
		return nil
	}
	if m.logger.GetSink() != nil {
		m.logger.V(1).Info("Extracting tools from MCPToolRegistry with schemas", "toolCount", m.mcpRegistry.GetToolCount())
	}
	funcDecls := m.mcpRegistry.GetToolsAsFunctionDeclarations()
	ensureToolSchema(funcDecls, m.logger)
	if len(funcDecls) == 0 {
		return nil
	}
	if m.logger.GetSink() != nil {
		m.logger.V(1).Info("Extracted tools from MCPToolRegistry", "toolsCount", 1, "totalFunctionDeclarations", len(funcDecls))
	}
	return []models.Tool{{FunctionDeclarations: funcDecls}}
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

// functionResponseToMap converts FunctionResponse.Response to map[string]any for genai (ADK expects map).
func (m *ModelAdapter) functionResponseToMap(fr *models.FunctionResponse) map[string]any {
	if fr == nil {
		return nil
	}
	if m.logger.GetSink() != nil {
		preview := ""
		if fr.Response != nil {
			if b, err := json.Marshal(fr.Response); err == nil {
				preview = string(b)
				if len(preview) > 1000 {
					preview = preview[:1000] + "... (truncated)"
				}
			} else {
				preview = fmt.Sprintf("%v", fr.Response)
			}
		}
		m.logger.Info("Converting function response to genai.Part",
			"functionName", fr.Name, "functionID", fr.ID, "responseType", fmt.Sprintf("%T", fr.Response), "responsePreview", preview)
	}
	if respMap, ok := fr.Response.(map[string]interface{}); ok {
		if m.logger.GetSink() != nil {
			m.logger.V(1).Info("Function response is already a map", "functionName", fr.Name)
		}
		return respMap
	}
	if responseStr, ok := fr.Response.(string); ok {
		var jsonMap map[string]interface{}
		if err := json.Unmarshal([]byte(responseStr), &jsonMap); err == nil {
			if m.logger.GetSink() != nil {
				m.logger.V(1).Info("Function response string parsed as JSON object", "functionName", fr.Name)
			}
			return jsonMap
		}
		if m.logger.GetSink() != nil {
			m.logger.V(1).Info("Function response string wrapped in 'content' field", "functionName", fr.Name, "contentLength", len(responseStr))
		}
		return map[string]any{"content": responseStr}
	}
	if m.logger.GetSink() != nil {
		m.logger.V(1).Info("Function response wrapped in 'output' field", "functionName", fr.Name, "responseType", fmt.Sprintf("%T", fr.Response))
	}
	return map[string]any{"output": fr.Response}
}

// convertLLMResponse converts our internal LLMResponse to Google ADK LLMResponse
//
// RCA (stop after first tool call): TurnComplete must mean "this LLM response is complete"
// (stream done), not "conversation turn is complete". Python (_openai.py) always yields
// the final response with turn_complete=True (line 514), even when content has tool_calls.
// The runner then inspects content: if it has function_calls, it executes tools and
// continues the loop. Setting TurnComplete=false for tool-call responses caused the
// runner to treat the response as incomplete (wait for more chunks) and never execute tools.
func (m *ModelAdapter) convertLLMResponse(resp *models.LLMResponse) *model.LLMResponse {
	// Match Python: TurnComplete = true when this is the final response (non-partial).
	// The Google ADK runner uses content (presence of function_calls) to decide whether
	// to execute tools and continue the loop, not TurnComplete.
	turnComplete := !resp.Partial

	if m.logger.GetSink() != nil {
		hasFunctionCalls := resp.FinishReason == models.FinishReasonToolCalls ||
			(resp.Content != nil && func() bool {
				for _, part := range resp.Content.Parts {
					if part.FunctionCall != nil {
						return true
					}
				}
				return false
			}())
		m.logger.V(1).Info("Converted LLM response to ADK",
			"partial", resp.Partial,
			"finishReason", resp.FinishReason,
			"turnComplete", turnComplete,
			"hasFunctionCalls", hasFunctionCalls)
	}

	adkResp := &model.LLMResponse{
		Partial:      resp.Partial,
		TurnComplete: turnComplete,
	}

	if resp.ErrorCode != "" {
		// For Google ADK, errors are returned via the iterator, not in the response
		// But we'll set it here for completeness
		return adkResp
	}

	if resp.Content != nil {
		// Convert content
		parts := make([]*genai.Part, 0, len(resp.Content.Parts))
		for _, part := range resp.Content.Parts {
			if part.Text != nil {
				// Create Part with Text field
				genaiPart := &genai.Part{
					Text: *part.Text,
				}
				parts = append(parts, genaiPart)
			}
			// Convert function calls
			if part.FunctionCall != nil {
				// Log function call arguments for debugging
				if m.logger.GetSink() != nil {
					argsJSON := ""
					if part.FunctionCall.Args != nil {
						if argsBytes, err := json.Marshal(part.FunctionCall.Args); err == nil {
							argsJSON = string(argsBytes)
						} else {
							argsJSON = fmt.Sprintf("%v", part.FunctionCall.Args)
						}
					}
					m.logger.V(1).Info("Converting function call to genai.Part",
						"functionName", part.FunctionCall.Name,
						"functionID", part.FunctionCall.ID,
						"args", argsJSON,
						"argsType", fmt.Sprintf("%T", part.FunctionCall.Args),
						"argsCount", len(part.FunctionCall.Args))
				}
				genaiPart := genai.NewPartFromFunctionCall(
					part.FunctionCall.Name,
					part.FunctionCall.Args,
				)
				// Set the ID if present
				if part.FunctionCall.ID != "" {
					genaiPart.FunctionCall.ID = part.FunctionCall.ID
				}
				parts = append(parts, genaiPart)
			}
			// Convert function responses
			if part.FunctionResponse != nil {
				responseMap := m.functionResponseToMap(part.FunctionResponse)
				genaiPart := genai.NewPartFromFunctionResponse(part.FunctionResponse.Name, responseMap)
				if part.FunctionResponse.ID != "" {
					genaiPart.FunctionResponse.ID = part.FunctionResponse.ID
				}
				parts = append(parts, genaiPart)
			}
			// Convert inline data if present
			if part.InlineData != nil {
				genaiPart := genai.NewPartFromBytes(
					part.InlineData.Data,
					part.InlineData.MimeType,
				)
				parts = append(parts, genaiPart)
			}
			// Convert file data if present
			if part.FileData != nil {
				genaiPart := genai.NewPartFromURI(
					part.FileData.FileURI,
					part.FileData.MimeType,
				)
				parts = append(parts, genaiPart)
			}
		}
		if len(parts) > 0 {
			adkResp.Content = &genai.Content{
				Role:  resp.Content.Role, // Role is string in genai.Content
				Parts: parts,
			}
		}
	}

	// Map usage metadata
	if resp.UsageMetadata != nil {
		adkResp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(resp.UsageMetadata.PromptTokenCount),
			CandidatesTokenCount: int32(resp.UsageMetadata.CandidatesTokenCount),
			// TotalTokenCount is calculated automatically by genai
		}
	}

	// Map finish reason
	if resp.FinishReason != "" {
		// Convert our finish reason string to genai.FinishReason
		switch resp.FinishReason {
		case models.FinishReasonStop:
			adkResp.FinishReason = genai.FinishReasonStop
		case models.FinishReasonMaxTokens:
			adkResp.FinishReason = genai.FinishReasonMaxTokens
		case models.FinishReasonSafety:
			adkResp.FinishReason = genai.FinishReasonSafety
		case models.FinishReasonToolCalls:
			// Google ADK/genai doesn't have a specific FinishReasonFunctionCall
			// Tool calls are indicated by the presence of FunctionCall in parts
			// We'll use FinishReasonStop as the default when tool calls are present
			adkResp.FinishReason = genai.FinishReasonStop
		case models.FinishReasonRecitation:
			adkResp.FinishReason = genai.FinishReasonRecitation
		default:
			// Unknown finish reason, leave as zero value
		}
	}

	return adkResp
}

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig
func CreateGoogleADKAgent(config *AgentConfig, logger logr.Logger) (agent.Agent, error) {
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

	// Create our internal model first
	switch m := config.Model.(type) {
	case *OpenAI:
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
		baseLLM, err := models.NewOpenAIModelWithLogger(modelConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI model: %w", err)
		}
		modelAdapter = NewModelAdapter(baseLLM, logger, mcpRegistry)
	case *AzureOpenAI:
		headers := extractHeaders(m.Headers)
		modelConfig := &models.AzureOpenAIConfig{
			Model:   m.Model,
			Headers: headers,
			Timeout: nil,
		}
		baseLLM, err := models.NewAzureOpenAIModelWithLogger(modelConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure OpenAI model: %w", err)
		}
		modelAdapter = NewModelAdapter(baseLLM, logger, mcpRegistry)
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

// CreateGoogleADKRunner creates a Google ADK Runner from AgentConfig
func CreateGoogleADKRunner(config *AgentConfig, sessionService SessionService, logger logr.Logger) (*runner.Runner, error) {
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

	// Create runner config
	appName := "kagent-app"
	if config.Description != "" {
		// Use description as app name hint
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

func fetchHttpTools(ctx context.Context, httpTools []HttpMcpServerConfig, mcpRegistry *MCPToolRegistry, logger logr.Logger) {
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

func fetchSseTools(ctx context.Context, sseTools []SseMcpServerConfig, mcpRegistry *MCPToolRegistry, logger logr.Logger) {
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
