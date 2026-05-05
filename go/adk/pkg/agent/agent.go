package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/tools"
	"github.com/kagent-dev/kagent/go/api/adk"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adkgemini "google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/loadmemorytool"
	"google.golang.org/adk/tool/preloadmemorytool"
	"google.golang.org/genai"
)

// Default model names used when not specified in configuration
const (
	DefaultGeminiModel    = "gemini-2.0-flash"
	DefaultAnthropicModel = "claude-sonnet-4-20250514"
	DefaultOllamaModel    = "llama3.2"
)

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig.
// agentName is used as the ADK agent identity (appears in event Author field).
// extraTools are appended to the agent's tool list (e.g. save_memory).
func CreateGoogleADKAgent(ctx context.Context, agentConfig *adk.AgentConfig, agentName string, extraTools ...tool.Tool) (agent.Agent, error) {
	a, _, err := CreateGoogleADKAgentWithSubagentSessionIDs(ctx, agentConfig, agentName, extraTools...)
	return a, err
}

// CreateGoogleADKAgentWithSubagentSessionIDs creates a Google ADK agent and a
// map of remote-subagent tool name → A2A context session ID (for stamping
// outbound A2A events). Callers that only need the agent can use
// CreateGoogleADKAgent.
func CreateGoogleADKAgentWithSubagentSessionIDs(ctx context.Context, agentConfig *adk.AgentConfig, agentName string, extraTools ...tool.Tool) (agent.Agent, map[string]string, error) {
	log := logr.FromContextOrDiscard(ctx)

	if agentConfig == nil {
		return nil, nil, fmt.Errorf("agent config is required")
	}

	toolsets := mcp.CreateToolsets(ctx, agentConfig.HttpTools, agentConfig.SseTools)
	subagentSessionIDs := make(map[string]string)

	var remoteAgentTools []tool.Tool
	for _, remoteAgent := range agentConfig.RemoteAgents {
		if remoteAgent.Url == "" {
			log.Info("Skipping remote agent with empty URL", "name", remoteAgent.Name)
			continue
		}
		remoteTool, sessionID, err := tools.NewKAgentRemoteA2ATool(remoteAgent.Name, remoteAgent.Description, remoteAgent.Url, nil, remoteAgent.Headers)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create remote A2A tool for %s: %w", remoteAgent.Name, err)
		}
		if sessionID != "" {
			subagentSessionIDs[remoteAgent.Name] = sessionID
		}
		remoteAgentTools = append(remoteAgentTools, remoteTool)
		log.Info("Wired remote A2A agent tool", "name", remoteAgent.Name, "url", remoteAgent.Url)
	}

	localTools, err := buildAgentTools(agentConfig, remoteAgentTools, extraTools, log)
	if err != nil {
		return nil, nil, err
	}

	if agentConfig.Model == nil {
		return nil, nil, fmt.Errorf("model configuration is required")
	}

	llmModel, err := CreateLLM(ctx, agentConfig.Model, log)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create LLM: %w", err)
	}

	if agentName == "" {
		agentName = "agent"
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
	// Strip synthetic HITL tool messages from the model request to avoid unnecessary token usage.
	beforeModelCallbacks := []llmagent.BeforeModelCallback{}
	if len(approvalSet) > 0 {
		log.Info("Wiring approval callback", "toolCount", len(approvalSet))
		beforeToolCallbacks = append(beforeToolCallbacks, MakeApprovalCallback(approvalSet))
		beforeModelCallbacks = append(beforeModelCallbacks, MakeStripConfirmationPartsCallback())
	}
	beforeToolCallbacks = append(beforeToolCallbacks, makeBeforeToolCallback(log))

	llmAgentConfig := llmagent.Config{
		Name:                 agentName,
		Description:          agentConfig.Description,
		Instruction:          agentConfig.Instruction,
		Model:                llmModel,
		IncludeContents:      llmagent.IncludeContentsDefault,
		Tools:                localTools,
		Toolsets:             toolsets,
		BeforeToolCallbacks:  beforeToolCallbacks,
		BeforeModelCallbacks: beforeModelCallbacks,
		AfterToolCallbacks: []llmagent.AfterToolCallback{
			makeAfterToolCallback(log),
		},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{
			makeOnToolErrorCallback(log),
		},
	}

	log.Info("Creating Google ADK LLM agent",
		"name", llmAgentConfig.Name,
		"hasDescription", llmAgentConfig.Description != "",
		"hasInstruction", llmAgentConfig.Instruction != "",
		"toolsCount", len(llmAgentConfig.Tools),
		"toolsetsCount", len(llmAgentConfig.Toolsets))

	llmAgent, err := llmagent.New(llmAgentConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	log.Info("Successfully created Google ADK LLM agent",
		"toolsCount", len(llmAgentConfig.Tools),
		"toolsetsCount", len(llmAgentConfig.Toolsets))

	return llmAgent, subagentSessionIDs, nil
}

func buildAgentTools(agentConfig *adk.AgentConfig, remoteAgentTools, extraTools []tool.Tool, log logr.Logger) ([]tool.Tool, error) {
	var localTools []tool.Tool
	if agentConfig.Memory != nil {
		log.Info("Memory configuration detected, adding memory tools")
		localTools = []tool.Tool{
			preloadmemorytool.New(),
			loadmemorytool.New(),
		}
	}
	localTools = append(localTools, remoteAgentTools...)
	localTools = append(localTools, extraTools...)

	skillsDirectory := strings.TrimSpace(os.Getenv("KAGENT_SKILLS_FOLDER"))
	if skillsDirectory != "" {
		skillsTools, err := tools.NewSkillsTools(skillsDirectory)
		if err != nil {
			return nil, fmt.Errorf("failed to create skills tools: %w", err)
		}
		localTools = append(localTools, skillsTools...)
		log.Info("Wired local skills tools", "skillsDirectory", skillsDirectory, "toolCount", len(skillsTools))
	}

	askUserTool, err := tools.NewAskUserTool()
	if err != nil {
		return nil, fmt.Errorf("failed to create ask_user tool: %w", err)
	}
	localTools = append(localTools, askUserTool)
	return localTools, nil
}

// CreateLLM creates an adkmodel.LLM from the model configuration.
// This is exported to allow reuse of model creation logic (e.g., for memory summarization).
func CreateLLM(ctx context.Context, m adk.Model, log logr.Logger) (adkmodel.LLM, error) {
	switch m := m.(type) {
	case *adk.OpenAI:
		cfg := &models.OpenAIConfig{
			TransportConfig:  transportConfigFromBase(m.BaseModel, m.Timeout),
			Model:            m.Model,
			BaseUrl:          m.BaseUrl,
			FrequencyPenalty: m.FrequencyPenalty,
			MaxTokens:        m.MaxTokens,
			N:                m.N,
			PresencePenalty:  m.PresencePenalty,
			ReasoningEffort:  m.ReasoningEffort,
			Seed:             m.Seed,
			Temperature:      m.Temperature,
			TopP:             m.TopP,
		}
		return models.NewOpenAIModelWithLogger(cfg, log)

	case *adk.AzureOpenAI:
		cfg := &models.AzureOpenAIConfig{
			TransportConfig: transportConfigFromBase(m.BaseModel, nil),
			Model:           m.Model,
		}
		return models.NewAzureOpenAIModelWithLogger(cfg, log)

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
		return models.NewAnthropicModelWithLogger(cfg, log)

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
		return models.NewOllamaModelWithLogger(cfg, log)

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
		// Use Bedrock Converse API for ALL models (including Anthropic)
		cfg := &models.BedrockConfig{
			TransportConfig:              transportConfigFromBase(m.BaseModel, nil),
			Model:                        modelName,
			Region:                       region,
			AdditionalModelRequestFields: m.AdditionalModelRequestFields,
		}
		return models.NewBedrockModelWithLogger(ctx, cfg, log)

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
		return models.NewAnthropicVertexAIModelWithLogger(ctx, cfg, region, project, log)

	case *adk.SAPAICore:
		cfg := models.SAPAICoreConfig{
			Model:         m.Model,
			BaseUrl:       m.BaseUrl,
			ResourceGroup: m.ResourceGroup,
			AuthUrl:       m.AuthUrl,
			Headers:       extractHeaders(m.Headers),
		}
		return models.NewSAPAICoreModelWithLogger(cfg, log)

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
func makeBeforeToolCallback(logger logr.Logger) llmagent.BeforeToolCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		logger.Info("Tool execution started",
			"tool", t.Name(),
			"functionCallID", ctx.FunctionCallID(),
			"sessionID", ctx.SessionID(),
			"invocationID", ctx.InvocationID(),
			"args", truncateArgs(args),
		)
		return nil, nil
	}
}

// makeAfterToolCallback returns an AfterToolCallback that logs tool completion.
func makeAfterToolCallback(logger logr.Logger) llmagent.AfterToolCallback {
	return func(ctx tool.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
		if err != nil {
			logger.Error(err, "Tool execution completed with error",
				"tool", t.Name(),
				"functionCallID", ctx.FunctionCallID(),
				"sessionID", ctx.SessionID(),
				"invocationID", ctx.InvocationID(),
			)
		} else {
			logger.Info("Tool execution completed",
				"tool", t.Name(),
				"functionCallID", ctx.FunctionCallID(),
				"sessionID", ctx.SessionID(),
				"invocationID", ctx.InvocationID(),
				"resultKeys", mapKeys(result),
			)
		}
		return nil, nil
	}
}

// makeOnToolErrorCallback returns an OnToolErrorCallback that logs tool errors.
func makeOnToolErrorCallback(logger logr.Logger) llmagent.OnToolErrorCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
		logger.Error(err, "Tool execution failed",
			"tool", t.Name(),
			"functionCallID", ctx.FunctionCallID(),
			"sessionID", ctx.SessionID(),
			"invocationID", ctx.InvocationID(),
			"args", truncateArgs(args),
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

// truncateArgs returns a JSON string of args truncated for safe logging.
func truncateArgs(args map[string]any) string {
	const (
		maxValueLen = 100
		maxTotalLen = 500
	)
	if args == nil {
		return "{}"
	}
	truncated := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > maxValueLen {
			truncated[k] = s[:maxValueLen] + "..."
		} else {
			truncated[k] = v
		}
	}
	b, err := json.Marshal(truncated)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	s := string(b)
	if len(s) > maxTotalLen {
		return s[:maxTotalLen] + "... (truncated)"
	}
	return s
}
