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

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
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
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", c.config.Provider)
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
