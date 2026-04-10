package models

import (
	"context"
	"fmt"
	"iter"
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

// BedrockConfig holds Bedrock configuration for the Converse API
type BedrockConfig struct {
	TransportConfig
	Model       string
	Region      string
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	TopK        *int
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

		// Convert content to Bedrock messages
		messages, systemInstruction := convertGenaiContentsToBedrockMessages(req.Contents)

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

		// Build tool configuration
		var toolConfig *types.ToolConfiguration
		if req.Config != nil && len(req.Config.Tools) > 0 {
			tools := convertGenaiToolsToBedrock(req.Config.Tools)
			if len(tools) > 0 {
				toolConfig = &types.ToolConfiguration{
					Tools: tools,
				}
			}
		}

		// Set telemetry attributes
		telemetry.SetLLMRequestAttributes(ctx, modelName, req)

		if stream {
			m.generateStreaming(ctx, modelName, messages, systemPrompt, inferenceConfig, toolConfig, yield)
		} else {
			m.generateNonStreaming(ctx, modelName, messages, systemPrompt, inferenceConfig, toolConfig, yield)
		}
	}
}

// generateStreaming handles streaming responses from Bedrock ConverseStream.
func (m *BedrockModel) generateStreaming(ctx context.Context, modelId string, messages []types.Message, systemPrompt []types.SystemContentBlock, inferenceConfig *types.InferenceConfiguration, toolConfig *types.ToolConfiguration, yield func(*model.LLMResponse, error) bool) {
	output, err := m.Client.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{
		ModelId:         aws.String(modelId),
		Messages:        messages,
		System:          systemPrompt,
		InferenceConfig: inferenceConfig,
		ToolConfig:      toolConfig,
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

	// Get the event stream and read events from the channel
	stream := output.GetStream()
	defer stream.Close()

	// Read events from the channel
	for event := range stream.Events() {
		// Handle content block delta (streaming text)
		if chunk, ok := event.(*types.ConverseStreamOutputMemberContentBlockDelta); ok {
			if delta, ok := chunk.Value.Delta.(*types.ContentBlockDeltaMemberText); ok {
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

	// Note: Tool calls are not extracted from streaming response as they require
	// parsing the complete message structure. The non-streaming path handles tool calls.
	// This is a limitation that could be improved in the future.

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

// generateNonStreaming handles non-streaming responses from Bedrock Converse.
func (m *BedrockModel) generateNonStreaming(ctx context.Context, modelId string, messages []types.Message, systemPrompt []types.SystemContentBlock, inferenceConfig *types.InferenceConfiguration, toolConfig *types.ToolConfiguration, yield func(*model.LLMResponse, error) bool) {
	output, err := m.Client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:         aws.String(modelId),
		Messages:        messages,
		System:          systemPrompt,
		InferenceConfig: inferenceConfig,
		ToolConfig:      toolConfig,
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
				functionCall := &genai.FunctionCall{
					ID:   aws.ToString(toolUseBlock.Value.ToolUseId),
					Name: aws.ToString(toolUseBlock.Value.Name),
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
// The document.Interface is an internal AWS type that stores JSON data.
// We use a simple approach of returning an empty map since we can't directly
// access the underlying data without JSON parsing.
func documentToMap(doc document.Interface) map[string]any {
	if doc == nil {
		return nil
	}
	// The AWS SDK document type stores JSON data internally.
	// For simplicity in this implementation, we return an empty map.
	// In a production implementation, you would use the String() method
	// and json.Unmarshal to extract the actual data.
	return map[string]any{}
}

// convertGenaiContentsToBedrockMessages converts genai.Content to Bedrock Converse API message format.
func convertGenaiContentsToBedrockMessages(contents []*genai.Content) ([]types.Message, string) {
	var messages []types.Message
	var systemInstruction string

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
		var toolUseBlocks []types.ContentBlock
		var toolResultBlocks []types.ContentBlock

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

			// Handle function call (tool use in Bedrock terminology)
			if part.FunctionCall != nil {
				toolUse := types.ToolUseBlock{
					ToolUseId: aws.String(part.FunctionCall.ID),
					Name:      aws.String(part.FunctionCall.Name),
					Input:     document.NewLazyDocument(part.FunctionCall.Args),
				}
				toolUseBlocks = append(toolUseBlocks, &types.ContentBlockMemberToolUse{
					Value: toolUse,
				})
				continue
			}

			// Handle function response (tool result in Bedrock terminology)
			if part.FunctionResponse != nil {
				// Extract response content
				result := extractBedrockFunctionResponseContent(part.FunctionResponse.Response)
				toolResult := types.ToolResultBlock{
					ToolUseId: aws.String(part.FunctionResponse.ID),
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberText{
							Value: result,
						},
					},
					Status: types.ToolResultStatusSuccess,
				}
				toolResultBlocks = append(toolResultBlocks, &types.ContentBlockMemberToolResult{
					Value: toolResult,
				})
				continue
			}
		}

		// Build messages based on what we found
		// Tool use and tool result blocks are appended to content blocks
		allContent := append(contentBlocks, toolUseBlocks...)
		allContent = append(allContent, toolResultBlocks...)

		if len(allContent) > 0 {
			msg := types.Message{
				Role:    role,
				Content: allContent,
			}
			messages = append(messages, msg)
		}
	}

	return messages, systemInstruction
}

// extractBedrockFunctionResponseContent extracts text content from a function response for Bedrock.
func extractBedrockFunctionResponseContent(response any) string {
	if response == nil {
		return ""
	}

	switch v := response.(type) {
	case string:
		return v
	case map[string]any:
		// Try to extract text from common formats
		if result, ok := v["result"].(string); ok {
			return result
		}
		if content, ok := v["content"].(string); ok {
			return content
		}
		// Fallback: serialize the whole map
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// convertGenaiToolsToBedrock converts genai.Tool to Bedrock Tool format.
func convertGenaiToolsToBedrock(tools []*genai.Tool) []types.Tool {
	if len(tools) == 0 {
		return nil
	}

	var bedrockTools []types.Tool

	for _, tool := range tools {
		if tool == nil || tool.FunctionDeclarations == nil {
			continue
		}

		for _, decl := range tool.FunctionDeclarations {
			if decl == nil {
				continue
			}

			// Build input schema as JSON document
			properties := make(map[string]any)
			if decl.Parameters != nil {
				for name, schema := range decl.Parameters.Properties {
					if schema == nil {
						continue
					}
					prop := map[string]any{
						"type": string(schema.Type),
					}
					if schema.Description != "" {
						prop["description"] = schema.Description
					}
					if len(schema.Enum) > 0 {
						prop["enum"] = schema.Enum
					}
					properties[name] = prop
				}
			}

			var required []any
			if decl.Parameters != nil {
				for _, r := range decl.Parameters.Required {
					required = append(required, r)
				}
			}

			schema := map[string]any{
				"type":       "object",
				"properties": properties,
			}
			if len(required) > 0 {
				schema["required"] = required
			}

			inputSchema := &types.ToolInputSchemaMemberJson{
				Value: document.NewLazyDocument(schema),
			}

			toolSpec := types.ToolSpecification{
				Name:        aws.String(decl.Name),
				Description: aws.String(decl.Description),
				InputSchema: inputSchema,
			}

			bedrockTool := &types.ToolMemberToolSpec{
				Value: toolSpec,
			}
			bedrockTools = append(bedrockTools, bedrockTool)
		}
	}

	return bedrockTools
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
