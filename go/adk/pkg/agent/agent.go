package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"buf.build/go/protovalidate"
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
	"google.golang.org/protobuf/types/known/durationpb"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/fileextract"
	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	adkoutputschema "github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	"github.com/kagent-dev/kagent/go/adk/pkg/tools"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// Default model names used when not specified in configuration
const (
	DefaultGeminiModel    = "gemini-3.5-flash"
	DefaultAnthropicModel = "claude-sonnet-4-6"
	DefaultOllamaModel    = "llama3.2"
)

// CreateGoogleADKAgent creates a Google ADK agent tree using the shared runtime settings.
// agentName is used as the ADK agent identity (appears in event Author field).
// extraTools are appended to the agent's tool list (e.g. save_memory).
// Optional stsPlugin can be provided for token propagation to MCP tools; pass
// nil if token propagation is not needed.
func CreateGoogleADKAgent(ctx context.Context, config *configv1.ADKConfig, agentName string, stsPlugin *sts.TokenPropagationPlugin, extraTools ...tool.Tool) (agent.Agent, error) {
	if config.GetAgent() == nil {
		return nil, fmt.Errorf("agent config is required")
	}
	if err := protovalidate.Validate(config); err != nil {
		return nil, err
	}
	return createGoogleADKAgent(ctx, config.Agent, agentName, stsPlugin, runtimeconfig.ProcessConfig(config), extraTools...)
}

func createGoogleADKAgent(ctx context.Context, agentConfig *configv1.ADKAgentConfig, agentName string, stsPlugin *sts.TokenPropagationPlugin, runtime *configv1.ADKRuntimeConfig, extraTools ...tool.Tool) (agent.Agent, error) {
	log := logging.FromContext(ctx)

	if agentConfig == nil {
		return nil, fmt.Errorf("agent config is required")
	}

	propagateToken := runtime.GetTokenPropagation().GetEnabled()
	var dynamicHeaderProvider mcp.DynamicHeaderProvider
	if stsPlugin != nil {
		dynamicHeaderProvider = stsPlugin.HeaderProvider
	}
	toolsets := mcp.CreateToolsets(ctx, agentConfig.HttpTools, agentConfig.SseTools, agentConfig.StdioTools, propagateToken, dynamicHeaderProvider)
	skillsDirectory := agentConfig.SkillsDirectory
	builtinTools := runtime.GetBuiltinTools()
	executionTools, err := tools.NewBuiltinTools(tools.BuiltinToolsConfig{
		SkillsDirectory: skillsDirectory, BashVenvPath: builtinTools.GetBashVenvPath(),
		DisableFileSearch: builtinTools.GetDisableFileSearch(), DisableBash: builtinTools.GetDisableBash(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create built-in tools: %w", err)
	}
	extraTools = append(extraTools, executionTools...)
	if skillsDirectory != "" {
		skillsSource := skill.NewFileSystemSource(os.DirFS(skillsDirectory))
		skills, err := skillsSource.ListFrontmatters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load skills: %w", err)
		}
		if len(skills) > 0 {
			skillsToolset, err := skilltoolset.New(ctx, skilltoolset.Config{Source: skillsSource})
			if err != nil {
				return nil, fmt.Errorf("failed to create skill toolset: %w", err)
			}
			toolsets = append(toolsets, skillsToolset)
			log.InfoContext(ctx, "wired local skills", "skills_directory", skillsDirectory, "skill_count", len(skills))
		}
	}
	mcpAppToolNames := mcp.MCPAppToolNamesFromToolsets(toolsets)

	var remoteAgentTools []tool.Tool
	for _, remoteAgent := range agentConfig.RemoteAgents {
		if remoteAgent.Url == "" {
			log.InfoContext(ctx, "skipping remote agent with empty URL", "name", remoteAgent.Name)
			continue
		}
		var httpClient *http.Client
		if remoteAgent.Timeout != nil {
			httpClient = &http.Client{Timeout: remoteAgent.Timeout.AsDuration()}
		}
		remoteTool, err := tools.NewKAgentRemoteA2ATool(remoteAgent.Name, remoteAgent.Description, remoteAgent.Url, httpClient, remoteAgent.Headers, propagateToken, remoteAgent.IsolateSessions)
		if err != nil {
			return nil, fmt.Errorf("failed to create remote A2A tool for %s: %w", remoteAgent.Name, err)
		}
		remoteAgentTools = append(remoteAgentTools, remoteTool)
		log.InfoContext(ctx, "wired remote A2A agent tool", "name", remoteAgent.Name, "url", remoteAgent.Url)
	}

	localTools, err := buildAgentTools(agentConfig, remoteAgentTools, extraTools, builtinTools.GetDisableAskUser(), log)
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
	llmModel = fileextract.WithFileText(llmModel)

	if agentName == "" {
		agentName = "agent"
	}
	var subAgents []agent.Agent
	for _, childConfig := range agentConfig.SubAgents {
		child, err := createGoogleADKAgent(ctx, childConfig, childConfig.Name, stsPlugin, runtime)
		if err != nil {
			return nil, fmt.Errorf("failed to create sub-agent %q: %w", childConfig.Name, err)
		}
		subAgents = append(subAgents, child)
	}

	// Build callbacks. MCP approval is attached to each toolset by CreateToolsets,
	// preserving the binding that selected the server without relying on tool names.
	beforeToolCallbacks := []llmagent.BeforeToolCallback{}
	beforeModelCallbacks := []llmagent.BeforeModelCallback{}

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
	if agentConfig.Output != nil {
		llmAgentConfig.OutputSchema, err = adkoutputschema.ToGenAISchema(agentConfig.Output.JsonSchema)
		if err != nil {
			return nil, err
		}
		// Provider adapters with native raw-schema fields consume the canonical
		// JSON Schema directly.Gemini must use only ADK's OutputSchema projection.
		if usesRawOutputSchema(agentConfig.Model) {
			if llmAgentConfig.GenerateContentConfig == nil {
				llmAgentConfig.GenerateContentConfig = &genai.GenerateContentConfig{}
			}
			var rawSchema any
			if err := json.Unmarshal(agentConfig.Output.JsonSchema, &rawSchema); err != nil {
				return nil, fmt.Errorf("decode raw output schema: %w", err)
			}
			llmAgentConfig.GenerateContentConfig.ResponseJsonSchema = rawSchema
			llmAgentConfig.GenerateContentConfig.ResponseMIMEType = "application/json"
		}
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

// usesRawOutputSchema reports whether the kagent provider adapter has a native
// raw JSON Schema request field. Gemini is intentionally absent: upstream ADK
// owns its native-vs-set_model_response selection using the typed projection.
func usesRawOutputSchema(model *configv1.ADKModelConfig) bool {
	switch model.GetProvider().(type) {
	case *configv1.ADKModelConfig_Openai, *configv1.ADKModelConfig_AzureOpenai, *configv1.ADKModelConfig_Anthropic, *configv1.ADKModelConfig_GeminiAnthropic, *configv1.ADKModelConfig_Bedrock, *configv1.ADKModelConfig_Foundry:
		return true
	default:
		return false
	}
}

func buildAgentTools(agentConfig *configv1.ADKAgentConfig, remoteAgentTools, extraTools []tool.Tool, disableAskUser bool, log *slog.Logger) ([]tool.Tool, error) {
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

	if !disableAskUser {
		askUserTool, err := tools.NewAskUserTool()
		if err != nil {
			return nil, fmt.Errorf("failed to create ask_user tool: %w", err)
		}
		localTools = append(localTools, askUserTool)
	}
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
func generateContentConfig(m *configv1.ADKModelConfig) *genai.GenerateContentConfig {
	var maxOutputTokens *int64
	switch p := m.GetProvider().(type) {
	case *configv1.ADKModelConfig_Gemini:
		maxOutputTokens = p.Gemini.MaxOutputTokens
	case *configv1.ADKModelConfig_GeminiVertexAi:
		maxOutputTokens = p.GeminiVertexAi.MaxOutputTokens
	}
	if maxOutputTokens == nil || *maxOutputTokens <= 0 {
		return nil
	}
	return &genai.GenerateContentConfig{MaxOutputTokens: int32(*maxOutputTokens)}
}

// CreateLLM creates an adkmodel.LLM from the model configuration.
// This is exported to allow reuse of model creation logic (e.g., for memory summarization).
func CreateLLM(ctx context.Context, m *configv1.ADKModelConfig) (adkmodel.LLM, error) {
	switch p := m.GetProvider().(type) {
	case *configv1.ADKModelConfig_Openai:
		cfg := &models.OpenAIConfig{
			TransportConfig:     transportConfigFromBase(m, modelDuration(p.Openai.Timeout)),
			Model:               m.Model,
			BaseUrl:             p.Openai.BaseUrl,
			FrequencyPenalty:    p.Openai.FrequencyPenalty,
			MaxTokens:           p.Openai.MaxTokens,
			MaxCompletionTokens: p.Openai.MaxCompletionTokens,
			N:                   p.Openai.N,
			PresencePenalty:     p.Openai.PresencePenalty,
			ReasoningEffort:     p.Openai.ReasoningEffort,
			Seed:                p.Openai.Seed,
			Temperature:         p.Openai.Temperature,
			TopP:                p.Openai.TopP,
			APIFormat:           p.Openai.ApiFormat,
			StrictOutputSchema:  new(p.Openai.StrictOutputSchema),
		}
		return models.NewOpenAIModel(ctx, cfg)

	case *configv1.ADKModelConfig_AzureOpenai:
		cfg := &models.AzureOpenAIConfig{
			TransportConfig:    transportConfigFromBase(m, nil),
			Model:              m.Model,
			Endpoint:           p.AzureOpenai.Endpoint,
			Deployment:         p.AzureOpenai.Deployment,
			APIVersion:         p.AzureOpenai.ApiVersion,
			APIFormat:          p.AzureOpenai.ApiFormat,
			StrictOutputSchema: new(p.AzureOpenai.StrictOutputSchema),
		}
		return models.NewAzureOpenAIModel(ctx, cfg)

	case *configv1.ADKModelConfig_Gemini:
		apiKey := env.GoogleAPIKey.Get()
		if apiKey == "" {
			apiKey = env.GeminiAPIKey.Get()
		}
		if apiKey == "" {
			return nil, fmt.Errorf("gemini model requires GOOGLE_API_KEY or GEMINI_API_KEY environment variable")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultGeminiModel
		}
		httpClient, err := models.BuildHTTPClient(transportConfigFromBase(m, nil))
		if err != nil {
			return nil, fmt.Errorf("failed to build HTTP client for Gemini: %w", err)
		}
		return adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{
			APIKey:     apiKey,
			HTTPClient: httpClient,
		})

	case *configv1.ADKModelConfig_GeminiVertexAi:
		project := env.GoogleCloudProject.Get()
		location := env.GoogleCloudLocation.Get()
		if location == "" {
			location = env.GoogleCloudRegion.Get()
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

	case *configv1.ADKModelConfig_Anthropic:
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultAnthropicModel
		}
		cfg := &models.AnthropicConfig{
			TransportConfig: transportConfigFromBase(m, modelDuration(p.Anthropic.Timeout)),
			Model:           modelName,
			BaseUrl:         p.Anthropic.BaseUrl,
			MaxTokens:       p.Anthropic.MaxTokens,
			Temperature:     p.Anthropic.Temperature,
			TopP:            p.Anthropic.TopP,
			TopK:            p.Anthropic.TopK,
			PromptCaching:   p.Anthropic.PromptCaching,
			CacheTTL:        p.Anthropic.CacheTtl,
		}
		return models.NewAnthropicModel(ctx, cfg)

	case *configv1.ADKModelConfig_Mistral:
		cfg := &models.MistralConfig{
			TransportConfig: transportConfigFromBase(m, modelDuration(p.Mistral.Timeout)),
			Model:           m.Model,
			BaseUrl:         p.Mistral.BaseUrl,
			MaxTokens:       p.Mistral.MaxTokens,
			Temperature:     p.Mistral.Temperature,
			TopP:            p.Mistral.TopP,
		}
		return models.NewMistralModel(ctx, cfg)

	case *configv1.ADKModelConfig_Ollama:
		// Leave the host empty when the controller did not set one: NewOllamaModel
		// then applies the cloud/local routing. Defaulting it to localhost here
		// would look like an operator-chosen endpoint and pin every cloud model
		// to the local daemon.
		baseURL := env.OllamaAPIBase.Get()
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultOllamaModel
		}
		// Create OllamaConfig with native SDK support for Ollama-specific options
		cfg := &models.OllamaConfig{
			TransportConfig: transportConfigFromBase(m, nil),
			Model:           modelName,
			Host:            baseURL,
			// The environment holds only the gateway credential placeholder.
			APIKey:  env.OllamaAPIKey.Get(),
			Options: p.Ollama.Options,
		}
		return models.NewOllamaModel(ctx, cfg)

	case *configv1.ADKModelConfig_Bedrock:
		region := p.Bedrock.Region
		if region == "" {
			region = env.AWSRegion.Get()
		}
		if region == "" {
			return nil, fmt.Errorf("bedrock requires AWS_REGION environment variable or region in model config")
		}
		modelName := m.Model
		if modelName == "" {
			return nil, fmt.Errorf("bedrock requires a model name (e.g. global.anthropic.claude-sonnet-4-6)")
		}
		// Use Bedrock Converse API for ALL models (including Anthropic).
		// ReadTimeout maps to the overall HTTP client timeout (whole Converse
		// request) and ConnectTimeout to the dialer, mirroring the Python ADK's
		// botocore read/connect timeouts so the config is honored on both runtimes.
		tc := transportConfigFromBase(m, modelDuration(p.Bedrock.ReadTimeout))
		tc.ConnectTimeout = modelDuration(p.Bedrock.ConnectTimeout)
		additionalFields, err := models.DecodeBedrockRequestFields(p.Bedrock.GetAdditionalModelRequestFields())
		if err != nil {
			return nil, err
		}
		cfg := &models.BedrockConfig{
			TransportConfig:              tc,
			Model:                        modelName,
			Region:                       region,
			AdditionalModelRequestFields: additionalFields,
			PromptCaching:                p.Bedrock.PromptCaching,
			CacheTTL:                     p.Bedrock.CacheTtl,
		}
		if p.Bedrock.Guardrail != nil {
			cfg.GuardrailIdentifier = p.Bedrock.Guardrail.Identifier
			cfg.GuardrailVersion = p.Bedrock.Guardrail.Version
			cfg.GuardrailTrace = p.Bedrock.Guardrail.Trace
		}
		return models.NewBedrockModel(ctx, cfg)

	case *configv1.ADKModelConfig_GeminiAnthropic:
		// GeminiAnthropic = Claude models accessed through Google Cloud Vertex AI.
		// Uses the Anthropic SDK's built-in Vertex AI support with Application Default Credentials.
		project := env.GoogleCloudProject.Get()
		region := env.GoogleCloudLocation.Get()
		if region == "" {
			region = env.GoogleCloudRegion.Get()
		}
		if project == "" || region == "" {
			return nil, fmt.Errorf("GeminiAnthropic (Anthropic on Vertex AI) requires GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION environment variables")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = DefaultAnthropicModel
		}
		cfg := &models.AnthropicConfig{
			TransportConfig: transportConfigFromBase(m, nil),
			Model:           modelName,
		}
		return models.NewAnthropicVertexAIModel(ctx, cfg, region, project)

	case *configv1.ADKModelConfig_SapAiCore:
		cfg := models.SAPAICoreConfig{
			Model:         m.Model,
			BaseUrl:       p.SapAiCore.BaseUrl,
			ResourceGroup: p.SapAiCore.ResourceGroup,
			AuthUrl:       p.SapAiCore.AuthUrl,
			Headers:       extractHeaders(m.Headers),
		}
		return models.NewSAPAICoreModel(ctx, cfg)

	case *configv1.ADKModelConfig_Foundry:
		if p.Foundry.ApiFormat == runtimeconfig.FoundryAPIFormatAnthropic {
			// Claude on Foundry uses the Anthropic Messages API, not the
			// OpenAI-compatible surface, so it maps to an AnthropicModel.
			// The request model is the Foundry deployment name (the constructor
			// resolves and sets it).
			cfg := &models.AnthropicConfig{
				TransportConfig: transportConfigFromBase(m, nil),
				Model:           p.Foundry.Deployment,
			}
			return models.NewFoundryAnthropicModel(ctx, cfg, p.Foundry.Endpoint, p.Foundry.Deployment, nil)
		}
		cfg := &models.FoundryConfig{
			TransportConfig:    transportConfigFromBase(m, nil),
			Model:              m.Model,
			Endpoint:           p.Foundry.Endpoint,
			Deployment:         p.Foundry.Deployment,
			APIVersion:         p.Foundry.ApiVersion,
			StrictOutputSchema: new(p.Foundry.StrictOutputSchema),
		}
		return models.NewFoundryModel(ctx, cfg)

	default:
		return nil, fmt.Errorf("unsupported model type: %s", runtimeconfig.ModelType(m))
	}
}

// transportConfigFromBase builds a TransportConfig from the shared model fields.
func transportConfigFromBase(b *configv1.ADKModelConfig, timeout *time.Duration) models.TransportConfig {
	return models.TransportConfig{
		Headers:               extractHeaders(b.Headers),
		PassthroughHeaders:    b.PassthroughHeaders,
		TLSInsecureSkipVerify: b.TlsInsecureSkipVerify,
		TLSCACertPath:         b.TlsCaCertPath,
		TLSDisableSystemCAs:   b.TlsDisableSystemCas,
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

func modelDuration(value *durationpb.Duration) *time.Duration {
	if value == nil {
		return nil
	}
	return new(value.AsDuration())
}
