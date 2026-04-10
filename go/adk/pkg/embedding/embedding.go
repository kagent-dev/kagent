package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
	"google.golang.org/genai"
)

const (
	// TargetDimension is the required embedding dimension for Kagent memory storage (768)
	TargetDimension = 768
)

// Client generates embeddings using configured provider.
type Client struct {
	config     *adk.EmbeddingConfig
	httpClient *http.Client
}

// Config for creating an embedding client.
type Config struct {
	EmbeddingConfig *adk.EmbeddingConfig
	HTTPClient      *http.Client
}

// New creates a new embedding client.
func New(cfg Config) (*Client, error) {
	if cfg.EmbeddingConfig == nil {
		return nil, fmt.Errorf("embedding config is required")
	}

	if cfg.EmbeddingConfig.Model == "" {
		return nil, fmt.Errorf("embedding model is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	return &Client{
		config:     cfg.EmbeddingConfig,
		httpClient: client,
	}, nil
}

// Generate generates embeddings for the given texts.
// Returns a slice of embedding vectors, one per input text.
// Each vector is 768-dimensional (truncated/normalized if needed).
func (c *Client) Generate(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided")
	}

	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Generating embeddings", "count", len(texts), "model", c.config.Model)

	// Route to appropriate provider
	switch c.config.Provider {
	case "openai", "":
		return c.generateOpenAI(ctx, texts)
	case "azure_openai":
		return c.generateAzureOpenAI(ctx, texts)
	case "ollama":
		return c.generateOllama(ctx, texts)
	case "gemini", "vertex_ai":
		return c.generateGemini(ctx, texts)
	case "bedrock":
		return c.generateBedrock(ctx, texts)
	default:
		// Unknown provider - try OpenAI-compatible as fallback
		return c.generateOpenAI(ctx, texts)
	}
}

// generateOpenAI generates embeddings using OpenAI API.
func (c *Client) generateOpenAI(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	baseURL := c.config.BaseUrl
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	url := fmt.Sprintf("%s/embeddings", baseURL)

	reqBody := map[string]any{
		"input":      texts,
		"model":      c.config.Model,
		"dimensions": TargetDimension,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set authentication header (OpenAI uses Bearer token)
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract and process embeddings
	embeddings := make([][]float32, 0, len(result.Data))
	for _, item := range result.Data {
		embedding := item.Embedding

		// Ensure correct dimension
		if len(embedding) > TargetDimension {
			log.V(1).Info("Truncating embedding", "from", len(embedding), "to", TargetDimension)
			embedding = embedding[:TargetDimension]
			embedding = normalizeL2(embedding)
		} else if len(embedding) < TargetDimension {
			return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
		}

		embeddings = append(embeddings, embedding)
	}

	log.Info("Successfully generated embeddings", "count", len(embeddings))
	return embeddings, nil
}

// generateAzureOpenAI generates embeddings using Azure OpenAI API.
func (c *Client) generateAzureOpenAI(ctx context.Context, texts []string) ([][]float32, error) {
	// Azure OpenAI uses same format as OpenAI but different endpoint structure
	// BaseUrl should be the full deployment URL
	if c.config.BaseUrl == "" {
		return nil, fmt.Errorf("base_url is required for Azure OpenAI")
	}

	url := fmt.Sprintf("%s/embeddings", c.config.BaseUrl)

	reqBody := map[string]any{
		"input": texts,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set authentication header (Azure uses api-key header)
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if apiKey != "" {
		req.Header.Set("api-key", apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Process embeddings same as OpenAI
	embeddings := make([][]float32, 0, len(result.Data))
	for _, item := range result.Data {
		embedding := item.Embedding

		if len(embedding) > TargetDimension {
			embedding = embedding[:TargetDimension]
			embedding = normalizeL2(embedding)
		}

		embeddings = append(embeddings, embedding)
	}

	return embeddings, nil
}

// generateOllama generates embeddings using Ollama API.
// Ollama's /v1/embeddings endpoint is OpenAI-compatible.
func (c *Client) generateOllama(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	// Get Ollama API base URL
	baseURL := c.config.BaseUrl
	if baseURL == "" {
		baseURL = os.Getenv("OLLAMA_API_BASE")
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	// Build URL for OpenAI-compatible endpoint
	url := fmt.Sprintf("%s/v1/embeddings", strings.TrimSuffix(baseURL, "/"))

	reqBody := map[string]any{
		"input": texts,
		"model": c.config.Model,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Ollama doesn't require API key, but accept one if provided

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract and process embeddings
	embeddings := make([][]float32, 0, len(result.Data))
	for _, item := range result.Data {
		embedding := item.Embedding

		// Ensure correct dimension
		if len(embedding) > TargetDimension {
			log.V(1).Info("Truncating embedding", "from", len(embedding), "to", TargetDimension)
			embedding = embedding[:TargetDimension]
			embedding = normalizeL2(embedding)
		} else if len(embedding) < TargetDimension {
			return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
		}

		embeddings = append(embeddings, embedding)
	}

	log.Info("Successfully generated embeddings with Ollama", "count", len(embeddings))
	return embeddings, nil
}

// generateGemini generates embeddings using Google Gemini/Vertex AI API.
func (c *Client) generateGemini(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	// Create genai client
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	// Call the embedding API with dimensionality parameter
	// Note: This uses the same approach as Python - calling EmbedContent with OutputDimensionality
	targetDim := int32(TargetDimension)
	embeddingResults := make([][]float32, len(texts))

	for i, text := range texts {
		// Use genai.Text to create the content
		content := genai.Text(text)
		result, err := client.Models.EmbedContent(ctx, c.config.Model, content, &genai.EmbedContentConfig{
			OutputDimensionality: &targetDim,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for text %d: %w", i, err)
		}

		if len(result.Embeddings) > 0 {
			embedding := result.Embeddings[0].Values
			// Convert to float32
			emb32 := make([]float32, len(embedding))
			for j, v := range embedding {
				emb32[j] = float32(v)
			}
			embeddingResults[i] = emb32
		}
	}

	log.Info("Successfully generated embeddings with Gemini", "count", len(embeddingResults))
	return embeddingResults, nil
}

// generateBedrock generates embeddings using the AWS Bedrock Titan Embedding API.
// Each text is embedded individually because the Titan Embedding API accepts
// a single inputText per invocation.
func (c *Client) generateBedrock(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	region := os.Getenv("AWS_DEFAULT_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := bedrockruntime.NewFromConfig(awsCfg)

	embeddings := make([][]float32, 0, len(texts))
	for i, text := range texts {
		reqBody, err := json.Marshal(map[string]string{"inputText": text})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request for text %d: %w", i, err)
		}

		output, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(c.config.Model),
			Body:        reqBody,
			ContentType: aws.String("application/json"),
			Accept:      aws.String("application/json"),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to invoke Bedrock model for text %d: %w", i, err)
		}

		var result bedrockEmbeddingResponse
		if err := json.Unmarshal(output.Body, &result); err != nil {
			return nil, fmt.Errorf("failed to decode Bedrock response for text %d: %w", i, err)
		}

		embedding := result.Embedding
		if len(embedding) > TargetDimension {
			log.V(1).Info("Truncating embedding", "from", len(embedding), "to", TargetDimension)
			embedding = embedding[:TargetDimension]
			embedding = normalizeL2(embedding)
		} else if len(embedding) < TargetDimension {
			return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
		}

		embeddings = append(embeddings, embedding)
	}

	log.Info("Successfully generated embeddings with Bedrock", "count", len(embeddings))
	return embeddings, nil
}

type bedrockEmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// normalizeL2 normalizes a vector to unit length using L2 norm.
func normalizeL2(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}

	norm := math.Sqrt(sum)
	if norm == 0 {
		return vec
	}

	normalized := make([]float32, len(vec))
	for i, v := range vec {
		normalized[i] = float32(float64(v) / norm)
	}

	return normalized
}

// OpenAI API response types

type openAIEmbeddingResponse struct {
	Data  []openAIEmbeddingData `json:"data"`
	Model string                `json:"model"`
	Usage openAIUsage           `json:"usage"`
}

type openAIEmbeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
