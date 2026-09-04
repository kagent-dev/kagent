package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"log/slog"

	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	"github.com/kagent-dev/kagent/go/adk/pkg/tools"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/internal/logging"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	adkgemini "google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/loadmemorytool"
	"google.golang.org/adk/v2/tool/preloadmemorytool"
	"google.golang.org/adk/v2/tool/skilltoolset"
	"google.golang.org/adk/v2/tool/skilltoolset/skill"
	"google.golang.org/genai"
)

// Default model names used when not specified in configuration
const (
	DefaultGeminiModel    = "gemini-2.5-flash"
	DefaultAnthropicModel = "claude-sonnet-4-20250514"
	DefaultOllamaModel    = "llama3.2"
)

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig.
// agentName is used as the ADK agent identity (appears in event Author field).
// extraTools are appended to the agent's tool list (e.g. save_memory).
// Optional stsPlugin can be provided for token propagation to MCP tools; pass
// nil if token propagation is not needed.
func CreateGoogleADKAgent(ctx context.Context, agentConfig *adk.AgentConfig, agentName string, stsPlugin *sts.TokenPropagationPlugin, extraTools ...tool.Tool) (agent.Agent, error) {
	return createGoogleADKAgent(ctx, agentConfig, agentName, stsPlugin, true, extraTools...)
}

func createGoogleADKAgent(ctx context.Context, agentConfig *adk.AgentConfig, agentName string, stsPlugin *sts.TokenPropagationPlugin, legacySkillsEnv bool, extraTools ...tool.Tool) (agent.Agent, error) {
	log := logging.FromContext(ctx)

	if agentConfig == nil {
		return nil, fmt.Errorf("agent config is required")
	}

	propagateToken := strings.ToLower(os.Getenv("KAGENT_PROPAGATE_TOKEN")) == "true"
	var dynamicHeaderProvider mcp.DynamicHeaderProvider
	if stsPlugin != nil {
		dynamicHeaderProvider = stsPlugin.HeaderProvider
	}
	toolsets := mcp.CreateToolsets(ctx, agentConfig.HttpTools, agentConfig.SseTools, agentConfig.StdioTools, propagateToken, dynamicHeaderProvider)
	skillsDirectory := agentConfig.SkillsDirectory
	if skillsDirectory == "" && legacySkillsEnv {
		skillsDirectory = strings.TrimSpace(os.Getenv("KAGENT_SKILLS_FOLDER"))
	}
	if skillsDirectory != "" {
		skillsSource := skill.NewFileSystemSource(os.DirFS(skillsDirectory))
		skills, err := skillsSource.ListFrontmatters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load skills: %w", err)
		}
		if len(skills) > 0 {
			executionTools, err := tools.NewSkillExecutionTools(skillsDirectory)
			if err != nil {
				return nil, fmt.Errorf("failed to create skill execution tools: %w", err)
			}
			extraTools = append(extraTools, executionTools...)

			skillsToolset, err := skilltoolset.New(ctx, skilltoolset.Config{Source: skillsSource})
			if err != nil {
				return nil, fmt.Errorf("failed to create skill toolset: %w", err)
			}
			toolsets = append(toolsets, skillsToolset)
			log.InfoContext(ctx, "wired local skills", "skills_directory", skillsDirectory, "skill_count", len(skills), "execution_tool_count", len(executionTools))
		}
	}
	mcpAppToolNames := mcp.MCPAppToolNamesFromToolsets(toolsets)

	var remoteAgentTools []tool.Tool
	for _, remoteAgent := range agentConfig.RemoteAgents {
		if remoteAgent.Url == "" {
			log.InfoContext(ctx, "skipping remote agent with empty URL", "name", remoteAgent.Name)
			continue
		}
		remoteTool, err := tools.NewKAgentRemoteA2ATool(remoteAgent.Name, remoteAgent.Description, remoteAgent.Url, nil, remoteAgent.Headers, propagateToken, remoteAgent.IsolateSessions)
		if err != nil {
			return nil, fmt.Errorf("failed to create remote A2A tool for %s: %w", remoteAgent.Name, err)
		}
		remoteAgentTools = append(remoteAgentTools, remoteTool)
		log.InfoContext(ctx, "wired remote A2A agent tool", "name", remoteAgent.Name, "url", remoteAgent.Url)
	}

	localTools, err := buildAgentTools(agentConfig, remoteAgentTools, extraTools, log)
	if err != nil {
		return nil, err
	}

	if agentConfig.Model == nil {
		return nil, fmt.Errorf("model configuration is required")
	}

	llmModel, err := CreateLLM(ctx, agentConfig.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM: %w", err)
	}

	if agentName == "" {
		agentName = "agent"
	}
	var subAgents []agent.Agent
	for _, childConfig := range agentConfig.SubAgents {
		child, err := createGoogleADKAgent(ctx, childConfig, childConfig.Name, stsPlugin, false)
		if err != nil {
			return nil, fmt.Errorf("failed to create sub-agent %q: %w", childConfig.Name, err)
		}
		subAgents = append(subAgents, child)
	}

	// Collect tool names that require approval from HttpTools and SseTools.
	approvalSet := make(map[string]bool)
	for _, ht := range agentConfig.HttpTools {
		for _, name := range ht.RequireApproval {
			approvalSet[name] = true
		}
	}
	for _, st := range agentConfig.SseTools {
		for _, name := range st.RequireApproval {
			approvalSet[name] = true
		}
	}

	// Build BeforeToolCallbacks. Approval gating runs first.
	beforeToolCallbacks := []llmagent.BeforeToolCallback{}
	beforeModelCallbacks := []llmagent.BeforeModelCallback{}

	if len(approvalSet) > 0 {
		log.InfoContext(ctx, "wiring approval callback", "tool_count", len(approvalSet))
		beforeToolCallbacks = append(beforeToolCallbacks, MakeApprovalCallback(approvalSet))
	}
	if len(mcpAppToolNames) > 0 {
		// For MCP App-capable tools, keep rich tool payloads in chat history for UI rendering,
		// but compact what is sent back to the model to avoid redundant polling/tool churn.
		log.InfoContext(ctx, "wiring MCP App model result callback", "tool_count", len(mcpAppToolNames))
		beforeModelCallbacks = append(beforeModelCallbacks, MakeMCPAppModelResultCallback(mcpAppToolNames))
	}
	beforeToolCallbacks = append(beforeToolCallbacks, makeBeforeToolCallback(log))

	llmAgentConfig := llmagent.Config{
		Name:                  agentName,
		Description:           agentConfig.Description,
		Instruction:           agentConfig.Instruction,
		Model:                 llmModel,
		GenerateContentConfig: generateContentConfig(agentConfig.Model),
		IncludeContents:       llmagent.IncludeContentsDefault,
		Tools:                 localTools,
		Toolsets:              toolsets,
		SubAgents:             subAgents,
		BeforeToolCallbacks:   beforeToolCallbacks,
		BeforeModelCallbacks:  beforeModelCallbacks,
		AfterToolCallbacks: []llmagent.AfterToolCallback{
			makeAfterToolCallback(log),
		},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{
			makeOnToolErrorCallback(log),
		},
	}

	log.InfoContext(ctx, "creating Google ADK LLM agent",
		"name", llmAgentConfig.Name,
		"has_description", llmAgentConfig.Description != "",
		"has_instruction", llmAgentConfig.Instruction != "",
		"tools_count", len(llmAgentConfig.Tools),
		"toolsets_count", len(llmAgentConfig.Toolsets))

	llmAgent, err := llmagent.New(llmAgentConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	log.InfoContext(ctx, "successfully created Google ADK LLM agent",
		"tools_count", len(llmAgentConfig.Tools),
		"toolsets_count", len(llmAgentConfig.Toolsets))

	return llmAgent, nil
}

func buildAgentTools(agentConfig *adk.AgentConfig, remoteAgentTools, extraTools []tool.Tool, log *slog.Logger) ([]tool.Tool, error) {
	var localTools []tool.Tool
	if agentConfig.Memory != nil {
		log.Info("memory configuration detected, adding memory tools")
		localTools = []tool.Tool{
			preloadmemorytool.New(),
			loadmemorytool.New(),
		}
	}
	localTools = append(localTools, remoteAgentTools...)
	localTools = append(localTools, extraTools...)

	askUserTool, err := tools.NewAskUserTool()
	if err != nil {
		return nil, fmt.Errorf("failed to create ask_user tool: %w", err)
	}
	localTools = append(localTools, askUserTool)
	return localTools, nil
}

// generateContentConfig returns the agent-level generation config derived from
// the model definition, or nil when the model doesn't specify any. ADK seeds
// each LLMRequest.Config from this value, so per-request mutations (e.g. in
// before-model callbacks) still take precedence.
//
// The native Gemini models read generation config from the per-request
// LLMRequest.Config rather than from the model definition, so a
// ModelConfig-level setting such as maxOutputTokens must be applied here.
func generateContentConfig(m adk.Model) *genai.GenerateContentConfig {
	var maxOutputTokens *int
	switch m := m.(type) {
	case *adk.Gemini:
		maxOutputTokens = m.MaxOutputTokens
	case *adk.GeminiVertexAI:
		maxOutputTokens = m.MaxOutputTokens
	}
	if maxOutputTokens == nil || *maxOutputTokens <= 0 {
		return nil
	}
	return &genai.GenerateContentConfig{MaxOutputTokens: int32(*maxOutputTokens)}
}

// CreateLLM creates an adkmodel.LLM from the model configuration.
// This is exported to allow reuse of model creation logic (e.g., for memory summarization).
func CreateLLM(ctx context.Context, m adk.Model) (adkmodel.LLM, error) {
	switch m := m.(type) {
	case *adk.OpenAI:
		cfg := &models.OpenAIConfig{
			TransportConfig:     transportConfigFromBase(m.BaseModel, m.Timeout),
			Model:               m.Model,
			BaseUrl:             m.BaseUrl,
			FrequencyPenalty:    m.FrequencyPenalty,
			MaxTokens:           m.MaxTokens,
			MaxCompletionTokens: m.MaxCompletionTokens,
			N:                   m.N,
			PresencePenalty:     m.PresencePenalty,
			ReasoningEffort:     m.ReasoningEffort,
			Seed:                m.Seed,
			Temperature:         m.Temperature,
			TopP:                m.TopP,
			APIFormat:           m.APIFormat,
		}
		return models.NewOpenAIModel(ctx, cfg)

	case *adk.AzureOpenAI:
		cfg := &models.AzureOpenAIConfig{
			TransportConfig: transportConfigFromBase(m.BaseModel, nil),
			Model:           m.Model,
			Endpoint:        m.Endpoint,
			Deployment:      m.Deployment,
			APIVersion:      m.APIVersion,
		}
		return models.NewAzureOpenAIModel(ctx, cfg)

	case *adk.Gemini:
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("gemini model requires GOOGLE_API_KEY or GEMINI_API_KEY environment variable")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultGeminiModel
		}
		httpClient, err := models.BuildHTTPClient(transportConfigFromBase(m.BaseModel, nil))
		if err != nil {
			return nil, fmt.Errorf("failed to build HTTP client for Gemini: %w", err)
		}
		return adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{
			APIKey:     apiKey,
			HTTPClient: httpClient,
		})

	case *adk.GeminiVertexAI:
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
			modelName = DefaultGeminiModel
		}
		return adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  project,
			Location: location,
		})

	case *adk.Anthropic:
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultAnthropicModel
		}
		cfg := &models.AnthropicConfig{
			TransportConfig: transportConfigFromBase(m.BaseModel, m.Timeout),
			Model:           modelName,
			BaseUrl:         m.BaseUrl,
			MaxTokens:       m.MaxTokens,
			Temperature:     m.Temperature,
			TopP:            m.TopP,
			TopK:            m.TopK,
		}
		return models.NewAnthropicModel(ctx, cfg)

	case *adk.Ollama:
		baseURL := os.Getenv("OLLAMA_API_BASE")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultOllamaModel
		}
		// Create OllamaConfig with native SDK support for Ollama-specific options
		cfg := &models.OllamaConfig{
			TransportConfig: transportConfigFromBase(m.BaseModel, nil),
			Model:           modelName,
			Host:            baseURL,
			Options:         m.Options,
		}
		return models.NewOllamaModel(ctx, cfg)

	case *adk.Bedrock:
		region := m.Region
		if region == "" {
			region = os.Getenv("AWS_REGION")
		}
		if region == "" {
			return nil, fmt.Errorf("bedrock requires AWS_REGION environment variable or region in model config")
		}
		modelName := m.Model
		if modelName == "" {
			return nil, fmt.Errorf("bedrock requires a model name (e.g. anthropic.claude-3-sonnet-20240229-v1:0)")
		}
		// Use Bedrock Converse API for ALL models (including Anthropic).
		// ReadTimeout maps to the overall HTTP client timeout (whole Converse
		// request) and ConnectTimeout to the dialer, mirroring the Python ADK's
		// botocore read/connect timeouts so the config is honored on both runtimes.
		tc := transportConfigFromBase(m.BaseModel, m.ReadTimeout)
		tc.ConnectTimeout = m.ConnectTimeout
		cfg := &models.BedrockConfig{
			TransportConfig:              tc,
			Model:                        modelName,
			Region:                       region,
			AdditionalModelRequestFields: m.AdditionalModelRequestFields,
			PromptCaching:                m.PromptCaching,
			CacheTTL:                     m.CacheTTL,
		}
		if m.Guardrail != nil {
			cfg.GuardrailIdentifier = m.Guardrail.Identifier
			cfg.GuardrailVersion = m.Guardrail.Version
			cfg.GuardrailTrace = m.Guardrail.Trace
		}
		return models.NewBedrockModel(ctx, cfg)

	case *adk.GeminiAnthropic:
		// GeminiAnthropic = Claude models accessed through Google Cloud Vertex AI.
		// Uses the Anthropic SDK's built-in Vertex AI support with Application Default Credentials.
		project := os.Getenv("GOOGLE_CLOUD_PROJECT")
		region := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if region == "" {
			region = os.Getenv("GOOGLE_CLOUD_REGION")
		}
		if project == "" || region == "" {
			return nil, fmt.Errorf("GeminiAnthropic (Anthropic on Vertex AI) requires GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION environment variables")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultAnthropicModel
		}
		cfg := &models.AnthropicConfig{
			TransportConfig: transportConfigFromBase(m.BaseModel, nil),
			Model:           modelName,
		}
		return models.NewAnthropicVertexAIModel(ctx, cfg, region, project)

	case *adk.SAPAICore:
		cfg := models.SAPAICoreConfig{
			Model:         m.Model,
			BaseUrl:       m.BaseUrl,
			ResourceGroup: m.ResourceGroup,
			AuthUrl:       m.AuthUrl,
			Headers:       extractHeaders(m.Headers),
		}
		return models.NewSAPAICoreModel(ctx, cfg)

	case *adk.Foundry:
		if m.APIFormat == adk.FoundryAPIFormatAnthropic {
			// Claude on Foundry uses the Anthropic Messages API, not the
			// OpenAI-compatible surface, so it maps to an AnthropicModel.
			// The request model is the Foundry deployment name (the constructor
			// resolves and sets it).
			cfg := &models.AnthropicConfig{
				TransportConfig: transportConfigFromBase(m.BaseModel, nil),
				Model:           m.Deployment,
			}
			return models.NewFoundryAnthropicModel(ctx, cfg, m.Endpoint, m.Deployment, nil)
		}
		cfg := &models.FoundryConfig{
			TransportConfig: transportConfigFromBase(m.BaseModel, nil),
			Model:           m.Model,
			Endpoint:        m.Endpoint,
			Deployment:      m.Deployment,
			APIVersion:      m.APIVersion,
		}
		return models.NewFoundryModel(ctx, cfg)

	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.GetType())
	}
}

// transportConfigFromBase builds a TransportConfig from the shared BaseModel fields.
func transportConfigFromBase(b adk.BaseModel, timeout *int) models.TransportConfig {
	return models.TransportConfig{
		Headers:               extractHeaders(b.Headers),
		TLSInsecureSkipVerify: b.TLSInsecureSkipVerify,
		TLSCACertPath:         b.TLSCACertPath,
		TLSDisableSystemCAs:   b.TLSDisableSystemCAs,
		APIKeyPassthrough:     b.APIKeyPassthrough,
		Timeout:               timeout,
	}
}

// extractHeaders returns an empty map if nil, the original map otherwise.
func extractHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return make(map[string]string)
	}
	return headers
}

// makeBeforeToolCallback returns a BeforeToolCallback that logs tool invocations.
func makeBeforeToolCallback(logger *slog.Logger) llmagent.BeforeToolCallback {
	return func(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		logger.InfoContext(ctx, "tool execution started",
			"tool", t.Name(),
			"function_call_id", ctx.FunctionCallID(),
			"session_id", ctx.SessionID(),
			"invocation_id", ctx.InvocationID(),
		)
		return nil, nil
	}
}

// makeAfterToolCallback returns an AfterToolCallback that logs tool completion.
func makeAfterToolCallback(logger *slog.Logger) llmagent.AfterToolCallback {
	return func(ctx agent.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
		if err != nil {
			logger.ErrorContext(ctx, "tool execution completed with error", "error", err,
				"tool", t.Name(),
				"function_call_id", ctx.FunctionCallID(),
				"session_id", ctx.SessionID(),
				"invocation_id", ctx.InvocationID(),
			)
		} else {
			logger.InfoContext(ctx, "tool execution completed",
				"tool", t.Name(),
				"function_call_id", ctx.FunctionCallID(),
				"session_id", ctx.SessionID(),
				"invocation_id", ctx.InvocationID(),
				"result_keys", mapKeys(result),
			)
		}
		return nil, nil
	}
}

// makeOnToolErrorCallback returns an OnToolErrorCallback that logs tool errors.
func makeOnToolErrorCallback(logger *slog.Logger) llmagent.OnToolErrorCallback {
	return func(ctx agent.Context, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
		logger.ErrorContext(ctx, "tool execution failed", "error", err,
			"tool", t.Name(),
			"function_call_id", ctx.FunctionCallID(),
			"session_id", ctx.SessionID(),
			"invocation_id", ctx.InvocationID(),
		)
		return nil, nil
	}
}

// mapKeys returns the top-level keys of a map for logging without exposing values.
func mapKeys(m map[string]any) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
