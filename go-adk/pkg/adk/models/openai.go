package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/sashabaranov/go-openai"
)

// OpenAIConfig holds OpenAI configuration
type OpenAIConfig struct {
	Model            string
	BaseUrl          string
	Headers          map[string]string // Default headers to pass to OpenAI API (matching Python default_headers)
	FrequencyPenalty *float64
	MaxTokens        *int
	N                *int
	PresencePenalty  *float64
	ReasoningEffort  *string
	Seed             *int
	Temperature      *float64
	Timeout          *int
	TopP             *float64
}

// AzureOpenAIConfig holds Azure OpenAI configuration
type AzureOpenAIConfig struct {
	Model   string
	Headers map[string]string // Default headers to pass to Azure OpenAI API (matching Python default_headers)
	Timeout *int
}

// OpenAIModel implements BaseLLM for OpenAI models
type OpenAIModel struct {
	Config      *OpenAIConfig
	Client      *openai.Client
	AzureClient *openai.Client // For Azure OpenAI
	IsAzure     bool
	Logger      logr.Logger
}

// NewOpenAIModel creates a new OpenAI model instance
func NewOpenAIModel(config *OpenAIConfig) (*OpenAIModel, error) {
	return NewOpenAIModelWithLogger(config, logr.Logger{})
}

// NewOpenAIModelWithLogger creates a new OpenAI model instance with a logger
func NewOpenAIModelWithLogger(config *OpenAIConfig, logger logr.Logger) (*OpenAIModel, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	clientConfig := openai.DefaultConfig(apiKey)
	if config.BaseUrl != "" {
		clientConfig.BaseURL = config.BaseUrl
	}

	// Set timeout if specified, otherwise use default
	if config.Timeout != nil {
		clientConfig.HTTPClient.Timeout = time.Duration(*config.Timeout) * time.Second
	} else {
		clientConfig.HTTPClient.Timeout = DefaultExecutionTimeout
	}

	// Set default headers if provided (matching Python: default_headers=self.default_headers)
	// go-openai ClientConfig uses OrgID and ProjectID for some headers, but for custom headers
	// we need to use a custom HTTP client with a transport that adds headers
	if len(config.Headers) > 0 {
		// Create a custom transport that adds headers to all requests
		originalTransport := clientConfig.HTTPClient.Transport
		if originalTransport == nil {
			originalTransport = http.DefaultTransport
		}

		clientConfig.HTTPClient.Transport = &headerTransport{
			base:    originalTransport,
			headers: config.Headers,
		}

		if logger.GetSink() != nil {
			logger.Info("Setting default headers for OpenAI client", "headersCount", len(config.Headers), "headers", config.Headers)
		}
	}

	// TODO: Add TLS configuration support (tls_disable_verify, tls_ca_cert_path, tls_disable_system_cas)
	// This would require custom HTTP client configuration

	client := openai.NewClientWithConfig(clientConfig)

	if logger.GetSink() != nil {
		logger.Info("Initialized OpenAI model", "model", config.Model, "baseUrl", config.BaseUrl)
	}

	return &OpenAIModel{
		Config:  config,
		Client:  client,
		IsAzure: false,
		Logger:  logger,
	}, nil
}

// NewAzureOpenAIModelWithLogger creates a new Azure OpenAI model instance with a logger
func NewAzureOpenAIModelWithLogger(config *AzureOpenAIConfig, logger logr.Logger) (*OpenAIModel, error) {
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	azureEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	apiVersion := os.Getenv("OPENAI_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}

	if apiKey == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_API_KEY environment variable is not set")
	}
	if azureEndpoint == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT environment variable is not set")
	}

	clientConfig := openai.DefaultAzureConfig(apiKey, azureEndpoint)
	clientConfig.APIVersion = apiVersion

	// Set Azure model mapper function (matching Python: model name is deployment name)
	// In Azure OpenAI, the model name in the request should be the deployment name
	// The mapper function maps the model name to the deployment name
	// Since config.Model should already be the deployment name, we return it as-is
	clientConfig.AzureModelMapperFunc = func(model string) string {
		// For Azure OpenAI, the model name should be the deployment name
		// If config.Model is set, use it; otherwise use the model parameter
		if config.Model != "" {
			if logger.GetSink() != nil {
				logger.Info("Mapping Azure OpenAI model to deployment", "model", model, "deployment", config.Model)
			}
			return config.Model
		}
		// Fallback: use model name directly (should be deployment name)
		if logger.GetSink() != nil {
			logger.Info("Using model name as Azure OpenAI deployment", "deployment", model)
		}
		return model
	}

	// Set timeout if specified, otherwise use default
	if config.Timeout != nil {
		clientConfig.HTTPClient.Timeout = time.Duration(*config.Timeout) * time.Second
	} else {
		clientConfig.HTTPClient.Timeout = DefaultExecutionTimeout
	}

	// Set default headers if provided (matching Python: default_headers=extra_headers)
	// Use a custom HTTP client transport to add headers to all requests
	if len(config.Headers) > 0 {
		// Create a custom transport that adds headers to all requests
		originalTransport := clientConfig.HTTPClient.Transport
		if originalTransport == nil {
			originalTransport = http.DefaultTransport
		}

		clientConfig.HTTPClient.Transport = &headerTransport{
			base:    originalTransport,
			headers: config.Headers,
		}

		if logger.GetSink() != nil {
			logger.Info("Setting default headers for Azure OpenAI client", "headersCount", len(config.Headers), "headers", config.Headers)
		}
	}

	// TODO: Add TLS configuration support

	client := openai.NewClientWithConfig(clientConfig)

	if logger.GetSink() != nil {
		logger.Info("Initialized Azure OpenAI model", "model", config.Model, "endpoint", azureEndpoint, "apiVersion", apiVersion)
	}

	return &OpenAIModel{
		Config:  &OpenAIConfig{Model: config.Model},
		Client:  client,
		IsAzure: true,
		Logger:  logger,
	}, nil
}

// GenerateContent generates content using OpenAI API
func (m *OpenAIModel) GenerateContent(ctx context.Context, request *LLMRequest, stream bool) (<-chan *LLMResponse, error) {
	ch := make(chan *LLMResponse, 10)

	go func() {
		defer close(ch)

		if m.Logger.GetSink() != nil {
			m.Logger.Info("Generating content", "model", request.Model, "stream", stream, "contentsCount", len(request.Contents))
		}

		// Convert system instruction
		systemInstruction := m.extractSystemInstruction(request.Config)
		if m.Logger.GetSink() != nil && systemInstruction != "" {
			m.Logger.Info("Using system instruction", "instructionLength", len(systemInstruction))
		}

		// Convert messages to OpenAI format
		messages, err := m.convertContentToOpenAIMessages(request.Contents, systemInstruction)
		if err != nil {
			if m.Logger.GetSink() != nil {
				m.Logger.Error(err, "Failed to convert messages to OpenAI format")
			}
			ch <- &LLMResponse{
				ErrorCode:    "CONVERSION_ERROR",
				ErrorMessage: fmt.Sprintf("Failed to convert messages: %v", err),
			}
			return
		}

		if m.Logger.GetSink() != nil {
			m.Logger.Info("Converted messages to OpenAI format", "messagesCount", len(messages))
		}

		// Prepare request
		// For Azure OpenAI, the model name in the request should be the deployment name
		// The AzureModelMapperFunc will map it, but we need to use the deployment name from config
		modelName := request.Model
		if m.IsAzure {
			// For Azure OpenAI, always use the deployment name from config
			// The model name in the request is the deployment name, not the model type
			if m.Config.Model != "" {
				modelName = m.Config.Model
			} else if modelName == "" {
				// Fallback: if no deployment name in config, use request model
				// This should not happen in normal operation
				if m.Logger.GetSink() != nil {
					m.Logger.Info("Warning: No deployment name in Azure OpenAI config, using request model", "model", modelName)
				}
			}
			if m.Logger.GetSink() != nil {
				m.Logger.Info("Using Azure OpenAI deployment name", "deployment", modelName)
			}
		} else {
			// For regular OpenAI, use request model or config model
			if modelName == "" {
				modelName = m.Config.Model
			}
		}

		req := openai.ChatCompletionRequest{
			Model:    modelName,
			Messages: messages,
		}

		// Add optional parameters
		if m.Config.Temperature != nil {
			req.Temperature = float32(*m.Config.Temperature)
		}
		if m.Config.MaxTokens != nil {
			req.MaxTokens = *m.Config.MaxTokens
		}
		if m.Config.TopP != nil {
			req.TopP = float32(*m.Config.TopP)
		}
		if m.Config.FrequencyPenalty != nil {
			req.FrequencyPenalty = float32(*m.Config.FrequencyPenalty)
		}
		if m.Config.PresencePenalty != nil {
			req.PresencePenalty = float32(*m.Config.PresencePenalty)
		}
		if m.Config.Seed != nil {
			seed := *m.Config.Seed
			req.Seed = &seed
		}
		if m.Config.N != nil {
			req.N = *m.Config.N
		}

		// Convert tools if present
		if request.Config != nil && len(request.Config.Tools) > 0 {
			tools := m.convertToolsToOpenAI(request.Config.Tools)
			if len(tools) > 0 {
				req.Tools = tools
				req.ToolChoice = "auto"
			}
		}

		if stream {
			if m.Logger.GetSink() != nil {
				m.Logger.Info("Starting streaming request", "model", req.Model, "messagesCount", len(req.Messages))
			}
			m.handleStreaming(ctx, ch, req)
		} else {
			if m.Logger.GetSink() != nil {
				m.Logger.Info("Starting non-streaming request", "model", req.Model, "messagesCount", len(req.Messages))
			}
			m.handleNonStreaming(ctx, ch, req)
		}
	}()

	return ch, nil
}

// extractSystemInstruction extracts system instruction from config
func (m *OpenAIModel) extractSystemInstruction(config *LLMRequestConfig) string {
	if config == nil || config.SystemInstruction == nil {
		return ""
	}

	switch v := config.SystemInstruction.(type) {
	case string:
		return v
	case Content:
		// Extract text from parts
		var textParts []string
		for _, part := range v.Parts {
			if part.Text != nil {
				textParts = append(textParts, *part.Text)
			}
		}
		return strings.Join(textParts, "\n")
	default:
		return ""
	}
}

// convertContentToOpenAIMessages converts Content list to OpenAI messages format
func (m *OpenAIModel) convertContentToOpenAIMessages(contents []Content, systemInstruction string) ([]openai.ChatCompletionMessage, error) {
	var messages []openai.ChatCompletionMessage

	// Add system message if provided
	if systemInstruction != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemInstruction,
		})
	}

	// Track function responses to match with tool calls
	functionResponses := make(map[string]FunctionResponse)

	// First pass: collect function responses
	for _, content := range contents {
		for _, part := range content.Parts {
			if part.FunctionResponse != nil {
				functionResponses[part.FunctionResponse.ID] = *part.FunctionResponse
			}
		}
	}

	// Second pass: convert contents to messages (system is already added from systemInstruction; skip any system-role content)
	for _, content := range contents {
		if content.Role == "system" {
			continue
		}
		role := m.convertRoleToOpenAI(content.Role)

		// Separate different types of parts
		var textParts []string
		var functionCalls []FunctionCall
		var imageParts []openai.ChatMessageImageURL

		for _, part := range content.Parts {
			if part.Text != nil {
				textParts = append(textParts, *part.Text)
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, *part.FunctionCall)
			} else if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") {
				// Convert inline image data
				imageData := base64.StdEncoding.EncodeToString(part.InlineData.Data)
				imageParts = append(imageParts, openai.ChatMessageImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, imageData),
				})
			}
		}

		// Handle function calls (assistant messages with tool_calls)
		// Matching Python: tool response messages must be added immediately after assistant message with tool_calls
		if len(functionCalls) > 0 && role == openai.ChatMessageRoleAssistant {
			toolCalls := make([]openai.ToolCall, 0, len(functionCalls))
			toolResponseMessages := make([]openai.ChatCompletionMessage, 0, len(functionCalls))

			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				toolCall := openai.ToolCall{
					ID:   fc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      fc.Name,
						Arguments: string(argsJSON),
					},
				}
				toolCalls = append(toolCalls, toolCall)

				// Check if we have a response for this tool call (matching Python lines 117-143)
				if fr, ok := functionResponses[fc.ID]; ok {
					// Extract content from function response
					// Matching Python: handle string, content array, or result field
					content := ""
					if responseStr, ok := fr.Response.(string); ok {
						content = responseStr
					} else if responseMap, ok := fr.Response.(map[string]interface{}); ok {
						if contentList, ok := responseMap["content"].([]interface{}); ok && len(contentList) > 0 {
							if contentItem, ok := contentList[0].(map[string]interface{}); ok {
								if text, ok := contentItem["text"].(string); ok {
									content = text
								}
							}
						} else if result, ok := responseMap["result"].(string); ok {
							content = result
						} else {
							// Fallback: marshal to JSON string
							if jsonBytes, err := json.Marshal(fr.Response); err == nil {
								content = string(jsonBytes)
							}
						}
					} else {
						// Fallback: marshal to JSON string
						if jsonBytes, err := json.Marshal(fr.Response); err == nil {
							content = string(jsonBytes)
						}
					}

					toolResponseMessages = append(toolResponseMessages, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						ToolCallID: fc.ID,
						Content:    content,
					})
				} else {
					// If no response is available, create a placeholder response
					// This prevents the OpenAI API error (matching Python lines 136-143)
					toolResponseMessages = append(toolResponseMessages, openai.ChatCompletionMessage{
						Role:       openai.ChatMessageRoleTool,
						ToolCallID: fc.ID,
						Content:    "No response available for this function call.",
					})
				}
			}

			// Combine text and tool calls
			contentParts := []openai.ChatMessagePart{}
			if len(textParts) > 0 {
				contentParts = append(contentParts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeText,
					Text: strings.Join(textParts, "\n"),
				})
			}
			if len(imageParts) > 0 {
				for _, img := range imageParts {
					contentParts = append(contentParts, openai.ChatMessagePart{
						Type:     openai.ChatMessagePartTypeImageURL,
						ImageURL: &img,
					})
				}
			}

			// Create assistant message with tool calls (matching Python lines 145-152)
			textContent := strings.Join(textParts, "\n")
			if textContent == "" {
				textContent = ""
			}
			msg := openai.ChatCompletionMessage{
				Role:      role,
				ToolCalls: toolCalls,
			}
			if len(contentParts) > 0 {
				msg.MultiContent = contentParts
			} else if textContent != "" {
				msg.Content = textContent
			}
			messages = append(messages, msg)

			// Add all tool response messages immediately after the assistant message
			// (matching Python lines 154-155: messages.extend(tool_response_messages))
			messages = append(messages, toolResponseMessages...)
		} else {
			// Handle regular text/image messages (only if no function calls)
			// (matching Python lines 157-178)
			// Regular text/image message
			contentParts := []openai.ChatMessagePart{}
			if len(textParts) > 0 {
				contentParts = append(contentParts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeText,
					Text: strings.Join(textParts, "\n"),
				})
			}
			if len(imageParts) > 0 {
				for _, img := range imageParts {
					contentParts = append(contentParts, openai.ChatMessagePart{
						Type:     openai.ChatMessagePartTypeImageURL,
						ImageURL: &img,
					})
				}
			}

			if len(contentParts) > 0 {
				messages = append(messages, openai.ChatCompletionMessage{
					Role:         role,
					MultiContent: contentParts,
				})
			} else if len(textParts) > 0 {
				// Fallback to simple text message
				messages = append(messages, openai.ChatCompletionMessage{
					Role:    role,
					Content: strings.Join(textParts, "\n"),
				})
			}
		}
	}

	return messages, nil
}

// convertRoleToOpenAI converts role to OpenAI format
func (m *OpenAIModel) convertRoleToOpenAI(role string) string {
	switch role {
	case "model", "assistant":
		return openai.ChatMessageRoleAssistant
	case "system":
		return openai.ChatMessageRoleSystem
	default:
		return openai.ChatMessageRoleUser
	}
}

// convertToolsToOpenAI converts tools to OpenAI format
func (m *OpenAIModel) convertToolsToOpenAI(tools []Tool) []openai.Tool {
	var openaiTools []openai.Tool

	for _, tool := range tools {
		for _, funcDecl := range tool.FunctionDeclarations {
			openaiTool := openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        funcDecl.Name,
					Description: funcDecl.Description,
					Parameters:  funcDecl.Parameters,
				},
			}
			openaiTools = append(openaiTools, openaiTool)
		}
	}

	return openaiTools
}

// handleStreaming handles streaming responses
func (m *OpenAIModel) handleStreaming(ctx context.Context, ch chan<- *LLMResponse, req openai.ChatCompletionRequest) {
	req.Stream = true

	if m.Logger.GetSink() != nil {
		m.Logger.Info("Creating streaming chat completion", "model", req.Model)
	}

	stream, err := m.Client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		if m.Logger.GetSink() != nil {
			m.Logger.Error(err, "Failed to create streaming chat completion")
		}
		ch <- &LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: fmt.Sprintf("Failed to create stream: %v", err),
		}
		return
	}
	defer stream.Close()

	if m.Logger.GetSink() != nil {
		m.Logger.Info("Streaming chat completion created, receiving responses")
	}

	var aggregatedText string
	var finishReason string
	var usageMetadata *UsageMetadata
	toolCallsAcc := make(map[int]map[string]interface{})

	for {
		response, err := stream.Recv()
		if err != nil {
			// EOF is a normal way to signal end of stream in Go (matching Python's async for completion)
			if errors.Is(err, io.EOF) {
				if m.Logger.GetSink() != nil {
					m.Logger.Info("Stream ended normally (EOF)")
				}
				break
			}
			// Check for "stream closed" error message as well
			if err.Error() == "stream closed" {
				if m.Logger.GetSink() != nil {
					m.Logger.Info("Stream closed normally")
				}
				break
			}
			// Check for context cancellation - this is expected when client disconnects
			if ctx.Err() == context.Canceled {
				if m.Logger.GetSink() != nil {
					m.Logger.Info("Stream canceled due to context cancellation (client may have disconnected)")
				}
				break
			}
			// Any other error is a real error
			if m.Logger.GetSink() != nil {
				m.Logger.Error(err, "Stream error occurred")
			}
			ch <- &LLMResponse{
				ErrorCode:    "STREAM_ERROR",
				ErrorMessage: fmt.Sprintf("Stream error: %v", err),
			}
			return
		}

		if len(response.Choices) == 0 {
			continue
		}

		// Use only the first choice (matching Python: chunk.choices[0])
		// OpenAI API can return multiple choices when n > 1, but for agent conversations
		// we only need a single response per turn. Additional choices are ignored.
		choice := response.Choices[0]
		delta := choice.Delta

		// Handle text content streaming
		if delta.Content != "" {
			aggregatedText += delta.Content
			ch <- &LLMResponse{
				Content: &Content{
					Role: "model",
					Parts: []Part{
						{Text: &delta.Content},
					},
				},
				Partial:      true,
				TurnComplete: choice.FinishReason != "",
			}
		}

		// Handle tool call chunks
		if len(delta.ToolCalls) > 0 {
			for _, toolCallChunk := range delta.ToolCalls {
				idx := 0
				if toolCallChunk.Index != nil {
					idx = *toolCallChunk.Index
				}
				if toolCallsAcc[idx] == nil {
					toolCallsAcc[idx] = make(map[string]interface{})
					toolCallsAcc[idx]["id"] = ""
					toolCallsAcc[idx]["name"] = ""
					toolCallsAcc[idx]["arguments"] = ""
				}

				if toolCallChunk.ID != "" {
					toolCallsAcc[idx]["id"] = toolCallChunk.ID
				}
				// Function is a struct, not a pointer, so check if it's not zero value
				if toolCallChunk.Function.Name != "" || toolCallChunk.Function.Arguments != "" {
					if toolCallChunk.Function.Name != "" {
						toolCallsAcc[idx]["name"] = toolCallChunk.Function.Name
					}
					if toolCallChunk.Function.Arguments != "" {
						toolCallsAcc[idx]["arguments"] = toolCallsAcc[idx]["arguments"].(string) + toolCallChunk.Function.Arguments
					}
				}
			}
		}

		if choice.FinishReason != "" {
			finishReason = string(choice.FinishReason)
		}

		// Handle usage metadata (streaming responses don't have Usage field)
		// Usage is only available in non-streaming responses
	}

	// Yield final aggregated response
	// IMPORTANT: Always send final response even if EOF occurred, to ensure tool calls are included
	finalParts := []Part{}

	if aggregatedText != "" {
		finalParts = append(finalParts, Part{Text: &aggregatedText})
	}

	// Add accumulated tool calls
	// Sort indices to ensure consistent ordering (matching Python: for idx in sorted(tool_calls_acc.keys()))
	toolCallIndices := make([]int, 0, len(toolCallsAcc))
	for idx := range toolCallsAcc {
		toolCallIndices = append(toolCallIndices, idx)
	}
	// Sort indices
	sort.Ints(toolCallIndices)

	for _, idx := range toolCallIndices {
		if tc, ok := toolCallsAcc[idx]; ok {
			argsStr, _ := tc["arguments"].(string)
			var args map[string]interface{}
			if argsStr != "" {
				if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
					if m.Logger.GetSink() != nil {
						m.Logger.Error(fmt.Errorf("failed to unmarshal tool call arguments"), "Failed to parse tool call arguments", "toolIndex", idx, "argumentsString", argsStr)
					}
					args = make(map[string]interface{}) // Fallback to empty map
				}
			} else {
				args = make(map[string]interface{})
			}

			name, _ := tc["name"].(string)
			id, _ := tc["id"].(string)

			if name != "" || id != "" {
				finalParts = append(finalParts, Part{
					FunctionCall: &FunctionCall{
						ID:   id,
						Name: name,
						Args: args,
					},
				})
			}
		}
	}

	// Map finish reason
	// If we have tool calls but no finish reason was captured, assume it's tool_calls
	// (matching Python behavior where tool_calls finish reason might not always be present)
	finalFinishReason := FinishReasonStop
	if finishReason == "" && len(finalParts) > 0 {
		// Check if any part is a function call
		hasFunctionCalls := false
		for _, part := range finalParts {
			if part.FunctionCall != nil {
				hasFunctionCalls = true
				break
			}
		}
		if hasFunctionCalls {
			finishReason = "tool_calls"
			if m.Logger.GetSink() != nil {
				m.Logger.Info("Inferred tool_calls finish reason from function calls in final response")
			}
		}
	}

	switch finishReason {
	case "length":
		finalFinishReason = FinishReasonMaxTokens
	case "content_filter":
		finalFinishReason = FinishReasonSafety
	case "tool_calls":
		finalFinishReason = FinishReasonToolCalls
	}

	if m.Logger.GetSink() != nil {
		m.Logger.Info("Streaming completed", "finishReason", finalFinishReason, "originalFinishReason", finishReason, "partsCount", len(finalParts), "toolCallsCount", len(toolCallsAcc), "aggregatedTextLength", len(aggregatedText))
		// Note: usage metadata is not available in streaming responses
	}

	// Always send final response, even if EOF occurred early
	// This ensures tool calls are included in the response
	ch <- &LLMResponse{
		Content: &Content{
			Role:  "model",
			Parts: finalParts,
		},
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  finalFinishReason,
		UsageMetadata: usageMetadata,
	}
}

// handleNonStreaming handles non-streaming responses
func (m *OpenAIModel) handleNonStreaming(ctx context.Context, ch chan<- *LLMResponse, req openai.ChatCompletionRequest) {
	req.Stream = false

	if m.Logger.GetSink() != nil {
		m.Logger.Info("Creating non-streaming chat completion", "model", req.Model)
	}

	response, err := m.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		if m.Logger.GetSink() != nil {
			m.Logger.Error(err, "Failed to create chat completion")
		}
		ch <- &LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: fmt.Sprintf("Failed to create completion: %v", err),
		}
		return
	}

	if m.Logger.GetSink() != nil {
		m.Logger.Info("Chat completion received", "choicesCount", len(response.Choices))
	}

	if len(response.Choices) == 0 {
		ch <- &LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: "No choices in response",
		}
		return
	}

	// Use only the first choice (matching Python: response.choices[0])
	// OpenAI API can return multiple choices when n > 1, but for agent conversations
	// we only need a single response per turn. Additional choices are ignored.
	// If multiple choices are needed, they would need to be handled at a higher level.
	if len(response.Choices) > 1 && m.Logger.GetSink() != nil {
		m.Logger.V(1).Info("Multiple choices in response, using first choice only", "choicesCount", len(response.Choices))
	}
	choice := response.Choices[0]
	message := choice.Message

	parts := []Part{}

	// Handle text content
	if message.Content != "" {
		text := message.Content
		parts = append(parts, Part{Text: &text})
	}

	// Handle tool calls
	if len(message.ToolCalls) > 0 {
		for _, toolCall := range message.ToolCalls {
			if toolCall.Type == openai.ToolTypeFunction {
				// Function is a struct, not a pointer, so check if it's not zero value
				if toolCall.Function.Name != "" {
					var args map[string]interface{}
					if toolCall.Function.Arguments != "" {
						_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
					}

					parts = append(parts, Part{
						FunctionCall: &FunctionCall{
							ID:   toolCall.ID,
							Name: toolCall.Function.Name,
							Args: args,
						},
					})
				}
			}
		}
	}

	// Map finish reason
	finishReason := FinishReasonStop
	switch choice.FinishReason {
	case openai.FinishReasonLength:
		finishReason = FinishReasonMaxTokens
	case openai.FinishReasonContentFilter:
		finishReason = FinishReasonSafety
	case openai.FinishReasonToolCalls:
		finishReason = FinishReasonToolCalls
	}

	// Handle usage metadata
	var usageMetadata *UsageMetadata
	// Usage is a struct, not a pointer, so check if it's not zero value
	if response.Usage.PromptTokens > 0 || response.Usage.CompletionTokens > 0 || response.Usage.TotalTokens > 0 {
		usageMetadata = &UsageMetadata{
			PromptTokenCount:     response.Usage.PromptTokens,
			CandidatesTokenCount: response.Usage.CompletionTokens,
			TotalTokenCount:      response.Usage.TotalTokens,
		}
	}

	if m.Logger.GetSink() != nil {
		m.Logger.Info("Non-streaming response processed", "finishReason", finishReason, "partsCount", len(parts))
		if usageMetadata != nil {
			m.Logger.Info("Token usage", "promptTokens", usageMetadata.PromptTokenCount, "completionTokens", usageMetadata.CandidatesTokenCount, "totalTokens", usageMetadata.TotalTokenCount)
		}
	}

	ch <- &LLMResponse{
		Content: &Content{
			Role:  "model",
			Parts: parts,
		},
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  finishReason,
		UsageMetadata: usageMetadata,
	}
}

// headerTransport wraps an http.RoundTripper and adds custom headers to all requests
// This is used to pass model headers to the OpenAI API (matching Python default_headers)
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip implements http.RoundTripper interface
func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	req = req.Clone(req.Context())

	// Add all custom headers to the request
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	// Use the base transport to execute the request
	return t.base.RoundTrip(req)
}
