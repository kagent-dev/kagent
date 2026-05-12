package models

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// bedrockToolIDValid matches Bedrock's toolUseId constraint: [a-zA-Z0-9_.:-]+
// bedrockToolNameInvalid matches characters not allowed in Bedrock tool names: [a-zA-Z0-9_-]+
var (
	bedrockToolIDValid     = regexp.MustCompile(`^[a-zA-Z0-9_.:-]+$`)
	bedrockToolIDInvalid   = regexp.MustCompile(`[^a-zA-Z0-9_.:-]`)
	bedrockToolNameInvalid = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
)

// sanitizeBedrockToolName returns a valid Bedrock tool name.
// Bedrock requires tool names to match [a-zA-Z0-9_-]+ and be non-empty.
// nameMap caches original->sanitized so repeated lookups for the same name are
// consistent. counter is incremented only when a synthetic name is needed.
func sanitizeBedrockToolName(name string, nameMap map[string]string, counter *int) string {
	if name == "" {
		*counter++
		return fmt.Sprintf("tool_fn_%d", *counter)
	}
	if sanitized, ok := nameMap[name]; ok {
		return sanitized
	}
	sanitized := bedrockToolNameInvalid.ReplaceAllString(name, "_")
	if sanitized == "" {
		*counter++
		sanitized = fmt.Sprintf("tool_fn_%d", *counter)
	}
	nameMap[name] = sanitized
	return sanitized
}

// sanitizeBedrockToolID returns a valid Bedrock toolUseId.
// Bedrock requires toolUseId to match [a-zA-Z0-9_.:-]+ and be non-empty.
// idMap caches original→sanitized so FunctionCall and FunctionResponse
// with the same original ID get the same sanitized ID, unless the ID is empty or fully-invalid.
func sanitizeBedrockToolID(id string, idMap map[string]string, counter *int) string {
	if id != "" {
		if sanitized, ok := idMap[id]; ok {
			return sanitized
		}
	}
	sanitized := bedrockToolIDInvalid.ReplaceAllString(id, "_")
	if !bedrockToolIDValid.MatchString(sanitized) {
		*counter++
		sanitized = fmt.Sprintf("tool_%d", *counter)
		return sanitized
	}
	idMap[id] = sanitized
	return sanitized
}

// BedrockConfig holds Bedrock configuration for the Converse API
type BedrockConfig struct {
	TransportConfig
	Model                        string
	Region                       string
	MaxTokens                    *int
	Temperature                  *float64
	TopP                         *float64
	AdditionalModelRequestFields map[string]any
}

// BedrockModel implements model.LLM for Amazon Bedrock using the Converse API.
// This supports all Bedrock model families (Anthropic, Amazon, Mistral, Cohere, etc.)
type BedrockModel struct {
	Config *BedrockConfig
	Client *bedrockruntime.Client
	Logger logr.Logger
}

// Name returns the model name.
func (m *BedrockModel) Name() string {
	return m.Config.Model
}

// NewBedrockModelWithLogger creates a new Bedrock model instance using the Converse API.
// Authentication uses AWS credentials (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, etc.)
// or IAM roles via the standard AWS SDK credential chain.
func NewBedrockModelWithLogger(ctx context.Context, config *BedrockConfig, logger logr.Logger) (*BedrockModel, error) {
	if config.Model == "" {
		return nil, fmt.Errorf("bedrock model name is required (e.g., anthropic.claude-3-sonnet-20240229-v1:0)")
	}

	region := config.Region
	if region == "" {
		return nil, fmt.Errorf("AWS region is required for Bedrock")
	}

	// Load AWS SDK configuration
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create HTTP client with TLS, passthrough, and header support
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bedrock HTTP client: %w", err)
	}

	// Create Bedrock runtime client
	client := bedrockruntime.NewFromConfig(awsCfg, func(o *bedrockruntime.Options) {
		o.HTTPClient = httpClient
	})

	if logger.GetSink() != nil {
		logger.Info("Initialized Bedrock Converse API model", "model", config.Model, "region", region)
	}

	return &BedrockModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}

// GenerateContent implements model.LLM for Bedrock models using the Converse API.
func (m *BedrockModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Get model name
		modelName := m.Config.Model
		if req.Model != "" {
			modelName = req.Model
		}

		// Build tool configuration first so nameMap is available for message conversion.
		// convertGenaiToolsToBedrock sanitizes tool names and returns the
		// original->sanitized mapping so the same mapping can be applied to
		// conversation history and reversed when restoring names from responses.
		var toolConfig *types.ToolConfiguration
		nameMap := make(map[string]string)
		if req.Config != nil && len(req.Config.Tools) > 0 {
			tools, nm := convertGenaiToolsToBedrock(req.Config.Tools)
			nameMap = nm
			if len(tools) > 0 {
				toolConfig = &types.ToolConfiguration{
					Tools: tools,
				}
			}
		}

		// Build reverse map for restoring original tool names from Bedrock responses.
		reverseNameMap := make(map[string]string, len(nameMap))
		for orig, sanitized := range nameMap {
			reverseNameMap[sanitized] = orig
		}

		// Convert content to Bedrock messages.
		// nameMap is passed so that any tool call recorded in conversation history
		// is written with the sanitized name Bedrock already knows about.
		messages, systemInstruction := convertGenaiContentsToBedrockMessages(req.Contents, nameMap)

		// Build inference config
		var inferenceConfig *types.InferenceConfiguration
		if m.Config.MaxTokens != nil || m.Config.Temperature != nil || m.Config.TopP != nil {
			inferenceConfig = &types.InferenceConfiguration{}
			if m.Config.MaxTokens != nil {
				inferenceConfig.MaxTokens = aws.Int32(int32(*m.Config.MaxTokens))
			}
			if m.Config.Temperature != nil {
				inferenceConfig.Temperature = aws.Float32(float32(*m.Config.Temperature))
			}
			if m.Config.TopP != nil {
				inferenceConfig.TopP = aws.Float32(float32(*m.Config.TopP))
			}
		}

		// Build system prompt
		var systemPrompt []types.SystemContentBlock
		if systemInstruction != "" {
			systemPrompt = append(systemPrompt, &types.SystemContentBlockMemberText{
				Value: systemInstruction,
			})
		}

		additionalFields := m.buildAdditionalModelRequestFields()

		// Set telemetry attributes
		telemetry.SetLLMRequestAttributes(ctx, modelName, req)

		if stream {
			m.generateStreaming(ctx, modelName, messages, systemPrompt, inferenceConfig, toolConfig, additionalFields, reverseNameMap, yield)
		} else {
			m.generateNonStreaming(ctx, modelName, messages, systemPrompt, inferenceConfig, toolConfig, additionalFields, reverseNameMap, yield)
		}
	}
}

// buildAdditionalModelRequestFields returns a document.Interface containing
// model-specific parameters that are not part of InferenceConfiguration.
// The raw map is forwarded as-is to the Bedrock Converse API.
// Returns nil when no extra fields are configured.
func (m *BedrockModel) buildAdditionalModelRequestFields() document.Interface {
	if len(m.Config.AdditionalModelRequestFields) == 0 {
		return nil
	}
	return document.NewLazyDocument(m.Config.AdditionalModelRequestFields)
}

// generateStreaming handles streaming responses from Bedrock ConverseStream.
// It properly handles both text and tool use content blocks during streaming.
// reverseNameMap maps sanitized Bedrock tool names back to their original names.
func (m *BedrockModel) generateStreaming(ctx context.Context, modelId string, messages []types.Message, systemPrompt []types.SystemContentBlock, inferenceConfig *types.InferenceConfiguration, toolConfig *types.ToolConfiguration, additionalFields document.Interface, reverseNameMap map[string]string, yield func(*model.LLMResponse, error) bool) {
	output, err := m.Client.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{
		ModelId:                      aws.String(modelId),
		Messages:                     messages,
		System:                       systemPrompt,
		InferenceConfig:              inferenceConfig,
		ToolConfig:                   toolConfig,
		AdditionalModelRequestFields: additionalFields,
	})

	if err != nil {
		yield(&model.LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: err.Error(),
		}, nil)
		return
	}

	var aggregatedText strings.Builder
	var finishReason genai.FinishReason
	var usageMetadata *genai.GenerateContentResponseUsageMetadata

	// Track tool calls during streaming
	// Map of content block index -> tool call being built
	toolCalls := make(map[int32]*streamingToolCall)
	var completedToolCalls []*genai.Part

	// Get the event stream and read events from the channel
	stream := output.GetStream()
	defer stream.Close()

	// Read events from the channel
	for event := range stream.Events() {
		// Handle content block start (tool use start)
		if start, ok := event.(*types.ConverseStreamOutputMemberContentBlockStart); ok {
			if toolStart, ok := start.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
				// A new tool use block is starting - initialize tracking
				blockIdx := aws.ToInt32(start.Value.ContentBlockIndex)
				toolCalls[blockIdx] = &streamingToolCall{
					ID:   aws.ToString(toolStart.Value.ToolUseId),
					Name: aws.ToString(toolStart.Value.Name),
				}
			}
		}

		// Handle content block delta (streaming text or tool input)
		if chunk, ok := event.(*types.ConverseStreamOutputMemberContentBlockDelta); ok {
			blockIdx := aws.ToInt32(chunk.Value.ContentBlockIndex)

			switch delta := chunk.Value.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				// Text content - yield immediately for streaming
				text := delta.Value
				aggregatedText.WriteString(text)

				response := &model.LLMResponse{
					Content: &genai.Content{
						Role: "model",
						Parts: []*genai.Part{
							{Text: text},
						},
					},
					Partial:      true,
					TurnComplete: false,
				}
				if !yield(response, nil) {
					return
				}

			case *types.ContentBlockDeltaMemberToolUse:
				// Tool use delta - accumulate the input JSON
				if tc, ok := toolCalls[blockIdx]; ok && delta.Value.Input != nil {
					tc.InputJSON += aws.ToString(delta.Value.Input)
				}
			}
		}

		// Handle content block stop (tool use complete)
		if stop, ok := event.(*types.ConverseStreamOutputMemberContentBlockStop); ok {
			blockIdx := aws.ToInt32(stop.Value.ContentBlockIndex)
			if tc, ok := toolCalls[blockIdx]; ok {
				// Tool use block completed - parse the accumulated JSON and create FunctionCall.
				// Restore the original tool name from the reverse map so the ADK framework
				// can dispatch to the correctly registered tool.
				originalName := tc.Name
				if orig, found := reverseNameMap[tc.Name]; found {
					originalName = orig
				}
				args := tc.parseArgs()
				functionCall := &genai.FunctionCall{
					ID:   tc.ID,
					Name: originalName,
					Args: args,
				}
				completedToolCalls = append(completedToolCalls, &genai.Part{FunctionCall: functionCall})
				delete(toolCalls, blockIdx) // Clean up
			}
		}

		// Handle message stop (includes stop reason)
		if stop, ok := event.(*types.ConverseStreamOutputMemberMessageStop); ok {
			finishReason = bedrockStopReasonToGenai(stop.Value.StopReason)
		}

		// Handle metadata event (includes usage)
		if meta, ok := event.(*types.ConverseStreamOutputMemberMetadata); ok {
			if meta.Value.Usage != nil {
				usageMetadata = &genai.GenerateContentResponseUsageMetadata{
					PromptTokenCount:     aws.ToInt32(meta.Value.Usage.InputTokens),
					CandidatesTokenCount: aws.ToInt32(meta.Value.Usage.OutputTokens),
					TotalTokenCount:      aws.ToInt32(meta.Value.Usage.TotalTokens),
				}
			}
		}
	}

	// Build final response
	finalParts := []*genai.Part{}
	text := aggregatedText.String()
	if text != "" {
		finalParts = append(finalParts, &genai.Part{Text: text})
	}
	// Add completed tool calls
	finalParts = append(finalParts, completedToolCalls...)

	response := &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: finalParts,
		},
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  finishReason,
		UsageMetadata: usageMetadata,
	}
	yield(response, nil)
}

// streamingToolCall tracks a tool call being built during streaming
type streamingToolCall struct {
	ID        string
	Name      string
	InputJSON string // Accumulated JSON input
}

// parseArgs parses the accumulated JSON input into a map
func (tc *streamingToolCall) parseArgs() map[string]any {
	if tc.InputJSON == "" {
		return map[string]any{}
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.InputJSON), &args); err != nil {
		// If unmarshal fails, return raw string wrapped in map
		return map[string]any{"_raw": tc.InputJSON}
	}
	return args
}

// generateNonStreaming handles non-streaming responses from Bedrock Converse.
// reverseNameMap maps sanitized Bedrock tool names back to their original names.
func (m *BedrockModel) generateNonStreaming(ctx context.Context, modelId string, messages []types.Message, systemPrompt []types.SystemContentBlock, inferenceConfig *types.InferenceConfiguration, toolConfig *types.ToolConfiguration, additionalFields document.Interface, reverseNameMap map[string]string, yield func(*model.LLMResponse, error) bool) {
	output, err := m.Client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:                      aws.String(modelId),
		Messages:                     messages,
		System:                       systemPrompt,
		InferenceConfig:              inferenceConfig,
		ToolConfig:                   toolConfig,
		AdditionalModelRequestFields: additionalFields,
	})

	if err != nil {
		yield(&model.LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: err.Error(),
		}, nil)
		return
	}

	// Extract content from output
	parts := []*genai.Part{}
	if message, ok := output.Output.(*types.ConverseOutputMemberMessage); ok {
		for _, block := range message.Value.Content {
			// Handle text content
			if textBlock, ok := block.(*types.ContentBlockMemberText); ok {
				parts = append(parts, &genai.Part{Text: textBlock.Value})
			}
			// Handle tool use content
			if toolUseBlock, ok := block.(*types.ContentBlockMemberToolUse); ok {
				// Restore the original tool name so the ADK framework can dispatch
				// to the correctly registered tool.
				toolName := aws.ToString(toolUseBlock.Value.Name)
				if orig, found := reverseNameMap[toolName]; found {
					toolName = orig
				}
				functionCall := &genai.FunctionCall{
					ID:   aws.ToString(toolUseBlock.Value.ToolUseId),
					Name: toolName,
				}
				// Convert document.Interface to map using the String() method and JSON parsing
				// The document type in AWS SDK implements String() that returns JSON
				if input := toolUseBlock.Value.Input; input != nil {
					functionCall.Args = documentToMap(input)
				}
				parts = append(parts, &genai.Part{FunctionCall: functionCall})
			}
		}
	}

	// Build finish reason
	finishReason := bedrockStopReasonToGenai(output.StopReason)

	// Build usage metadata
	var usageMetadata *genai.GenerateContentResponseUsageMetadata
	if output.Usage != nil {
		usageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     aws.ToInt32(output.Usage.InputTokens),
			CandidatesTokenCount: aws.ToInt32(output.Usage.OutputTokens),
			TotalTokenCount:      aws.ToInt32(output.Usage.TotalTokens),
		}
	}

	response := &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  finishReason,
		UsageMetadata: usageMetadata,
	}
	telemetry.SetLLMResponseAttributes(ctx, response)
	yield(response, nil)
}

// documentToMap converts an AWS document.Interface to a map[string]any.
// The document.Interface is an abstraction that stores JSON data internally.
func documentToMap(doc document.Interface) map[string]any {
	if doc == nil {
		return nil
	}

	// document.Interface provides UnmarshalSmithyDocument to decode JSON data
	var result map[string]any
	if err := doc.UnmarshalSmithyDocument(&result); err != nil {
		// If unmarshal fails, return empty map to avoid nil pointer issues
		return map[string]any{}
	}

	return result
}

// convertGenaiContentsToBedrockMessages converts genai.Content to Bedrock Converse API message format.
// nameMap is the original->sanitized tool name map produced by convertGenaiToolsToBedrock.
// Any FunctionCall found in the conversation history is written with the sanitized name so
// that Bedrock can correlate it with the tool spec it already received. A nil nameMap is safe.
func convertGenaiContentsToBedrockMessages(contents []*genai.Content, nameMap map[string]string) ([]types.Message, string) {
	var messages []types.Message
	var systemInstruction string

	// Sanitize tool IDs: Bedrock requires toolUseId to match [a-zA-Z0-9_.:-]+ (non-empty).
	// See https://github.com/kagent-dev/kagent/issues/1473
	idMap := make(map[string]string)
	idCounter := 0

	for _, content := range contents {
		if content == nil || len(content.Parts) == 0 {
			continue
		}

		// Determine role
		role := types.ConversationRoleUser
		if content.Role == "model" || content.Role == "assistant" {
			role = types.ConversationRoleAssistant
		}

		var contentBlocks []types.ContentBlock

		for _, part := range content.Parts {
			if part == nil {
				continue
			}

			// Handle text
			if part.Text != "" {
				// Check if this is a system message
				if content.Role == "system" {
					systemInstruction = part.Text
					continue
				}
				contentBlocks = append(contentBlocks, &types.ContentBlockMemberText{
					Value: part.Text,
				})
				continue
			}

			// Handle function call (tool use in Bedrock terminology).
			// Use the sanitized name from nameMap so Bedrock can correlate the
			// tool call with the tool spec sent in the same request.
			if part.FunctionCall != nil {
				callName := part.FunctionCall.Name
				if sanitized, ok := nameMap[callName]; ok {
					callName = sanitized
				}
				toolUse := types.ToolUseBlock{
					ToolUseId: aws.String(sanitizeBedrockToolID(part.FunctionCall.ID, idMap, &idCounter)),
					Name:      aws.String(callName),
					Input:     document.NewLazyDocument(part.FunctionCall.Args),
				}
				contentBlocks = append(contentBlocks, &types.ContentBlockMemberToolUse{
					Value: toolUse,
				})
				continue
			}

			// Handle function response (tool result in Bedrock terminology)
			if part.FunctionResponse != nil {
				// Extract response content
				result := extractFunctionResponseContent(part.FunctionResponse.Response)
				toolResult := types.ToolResultBlock{
					ToolUseId: aws.String(sanitizeBedrockToolID(part.FunctionResponse.ID, idMap, &idCounter)),
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberText{
							Value: result,
						},
					},
					Status: types.ToolResultStatusSuccess,
				}
				contentBlocks = append(contentBlocks, &types.ContentBlockMemberToolResult{
					Value: toolResult,
				})
				continue
			}
		}

		if len(contentBlocks) > 0 {
			messages = append(messages, types.Message{Role: role, Content: contentBlocks})
		}
	}

	return messages, systemInstruction
}

// convertGenaiToolsToBedrock converts genai.Tool to Bedrock Tool format.
// It sanitizes tool names to satisfy Bedrock's [a-zA-Z0-9_-]+ constraint and
// returns the original->sanitized name mapping so callers can apply it to
// conversation history and reverse it when restoring names from responses.
func convertGenaiToolsToBedrock(tools []*genai.Tool) ([]types.Tool, map[string]string) {
	if len(tools) == 0 {
		return nil, nil
	}

	nameMap := make(map[string]string)
	nameCounter := 0
	var bedrockTools []types.Tool

	for _, tool := range tools {
		if tool == nil || tool.FunctionDeclarations == nil {
			continue
		}

		for _, decl := range tool.FunctionDeclarations {
			if decl == nil {
				continue
			}

			// Build input schema as JSON document.
			// MCP tools and built-in local toolsets set ParametersJsonSchema.
			var schema map[string]any
			if decl.ParametersJsonSchema != nil {
				schema = parametersJsonSchemaToMap(decl.ParametersJsonSchema)
			}
			// Declaration-based tools set Parameters (genai.Schema with uppercase type names).
			if schema == nil && decl.Parameters != nil {
				// Marshal the full genai.Schema to JSON (preserves nested props, arrays, anyOf, etc.)
				// then lowercase all type values to match JSON Schema standard.
				schema = genaiSchemaToMap(decl.Parameters)
			}
			// Fallback to empty object if no schema is found.
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}

			inputSchema := &types.ToolInputSchemaMemberJson{
				Value: document.NewLazyDocument(schema),
			}

			// Sanitize the tool name: MCP tool names often contain dots, colons,
			// or spaces (e.g. "fetch.get_url") that Bedrock rejects.
			sanitizedName := sanitizeBedrockToolName(decl.Name, nameMap, &nameCounter)

			toolSpec := types.ToolSpecification{
				Name:        aws.String(sanitizedName),
				Description: aws.String(decl.Description),
				InputSchema: inputSchema,
			}

			bedrockTool := &types.ToolMemberToolSpec{
				Value: toolSpec,
			}
			bedrockTools = append(bedrockTools, bedrockTool)
		}
	}

	return bedrockTools, nameMap
}

// bedrockStopReasonToGenai maps Bedrock stop reason to genai.FinishReason.
func bedrockStopReasonToGenai(reason types.StopReason) genai.FinishReason {
	switch reason {
	case types.StopReasonMaxTokens:
		return genai.FinishReasonMaxTokens
	case types.StopReasonEndTurn, types.StopReasonStopSequence:
		return genai.FinishReasonStop
	case types.StopReasonToolUse:
		return genai.FinishReasonStop // Tool use is handled separately in content
	default:
		return genai.FinishReasonStop
	}
}
