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
	"sync"

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

// provider is the internal interface for per-provider embedding generation.
type provider interface {
	generate(ctx context.Context, texts []string) ([][]float32, error)
}

// Client generates embeddings using a configured provider.
type Client struct {
	config *adk.EmbeddingConfig
	p      provider
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
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		config: cfg.EmbeddingConfig,
		p:      newProvider(cfg.EmbeddingConfig, httpClient),
	}, nil
}

func newProvider(cfg *adk.EmbeddingConfig, httpClient *http.Client) provider {
	switch cfg.Provider {
	case "azure_openai":
		return &azureOpenAIProvider{config: cfg, httpClient: httpClient}
	case "ollama":
		return &ollamaProvider{config: cfg, httpClient: httpClient}
	case "gemini", "vertex_ai":
		return &geminiProvider{config: cfg}
	case "bedrock":
		return &bedrockProvider{config: cfg}
	default: // "openai", "", and unknown providers
		return &openAIProvider{config: cfg, httpClient: httpClient}
	}
}

// Generate generates embeddings for the given texts.
// Returns a slice of embedding vectors, one per input text.
// Each vector is 768-dimensional (truncated/normalized if needed).
func (c *Client) Generate(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided")
	}
	logr.FromContextOrDiscard(ctx).V(1).Info("Generating embeddings", "count", len(texts), "model", c.config.Model)
	return c.p.generate(ctx, texts)
}

type openAIProvider struct {
	config     *adk.EmbeddingConfig
	httpClient *http.Client
}

func (p *openAIProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	baseURL := p.config.BaseUrl
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	url := fmt.Sprintf("%s/embeddings", baseURL)

	reqBody := map[string]any{
		"input":      texts,
		"model":      p.config.Model,
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
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	}

	resp, err := p.httpClient.Do(req)
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

	embeddings := make([][]float32, 0, len(result.Data))
	for _, item := range result.Data {
		embedding := item.Embedding
		if len(embedding) > TargetDimension {
			log.V(1).Info("Truncating embedding", "from", len(embedding), "to", TargetDimension)
			embedding = normalizeL2(embedding[:TargetDimension])
		} else if len(embedding) < TargetDimension {
			return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
		}
		embeddings = append(embeddings, embedding)
	}
	log.Info("Successfully generated embeddings", "count", len(embeddings))
	return embeddings, nil
}

type azureOpenAIProvider struct {
	config     *adk.EmbeddingConfig
	httpClient *http.Client
}

func (p *azureOpenAIProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	if p.config.BaseUrl == "" {
		return nil, fmt.Errorf("base_url is required for Azure OpenAI")
	}
	url := fmt.Sprintf("%s/embeddings", p.config.BaseUrl)

	reqBody := map[string]any{"input": texts}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := os.Getenv("AZURE_OPENAI_API_KEY"); apiKey != "" {
		req.Header.Set("api-key", apiKey)
	}

	resp, err := p.httpClient.Do(req)
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

	embeddings := make([][]float32, 0, len(result.Data))
	for _, item := range result.Data {
		embedding := item.Embedding
		if len(embedding) > TargetDimension {
			embedding = normalizeL2(embedding[:TargetDimension])
		}
		embeddings = append(embeddings, embedding)
	}
	return embeddings, nil
}

type ollamaProvider struct {
	config     *adk.EmbeddingConfig
	httpClient *http.Client
}

func (p *ollamaProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	baseURL := p.config.BaseUrl
	if baseURL == "" {
		baseURL = os.Getenv("OLLAMA_API_BASE")
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	url := fmt.Sprintf("%s/v1/embeddings", strings.TrimSuffix(baseURL, "/"))

	reqBody := map[string]any{
		"input": texts,
		"model": p.config.Model,
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

	resp, err := p.httpClient.Do(req)
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

	embeddings := make([][]float32, 0, len(result.Data))
	for _, item := range result.Data {
		embedding := item.Embedding
		if len(embedding) > TargetDimension {
			log.V(1).Info("Truncating embedding", "from", len(embedding), "to", TargetDimension)
			embedding = normalizeL2(embedding[:TargetDimension])
		} else if len(embedding) < TargetDimension {
			return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
		}
		embeddings = append(embeddings, embedding)
	}
	log.Info("Successfully generated embeddings with Ollama", "count", len(embeddings))
	return embeddings, nil
}

type geminiProvider struct {
	config  *adk.EmbeddingConfig
	once    sync.Once
	client  *genai.Client
	initErr error
}

func (p *geminiProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	p.once.Do(func() {
		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey: os.Getenv("GOOGLE_API_KEY"),
		})
		if err != nil {
			p.initErr = fmt.Errorf("failed to create genai client: %w", err)
			return
		}
		p.client = client
	})
	if p.initErr != nil {
		return nil, p.initErr
	}

	targetDim := int32(TargetDimension)
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		result, err := p.client.Models.EmbedContent(ctx, p.config.Model, genai.Text(text), &genai.EmbedContentConfig{
			OutputDimensionality: &targetDim,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for text %d: %w", i, err)
		}
		if len(result.Embeddings) > 0 {
			src := result.Embeddings[0].Values
			emb := make([]float32, len(src))
			for j, v := range src {
				emb[j] = float32(v)
			}
			embeddings[i] = emb
		}
	}
	log.Info("Successfully generated embeddings with Gemini", "count", len(embeddings))
	return embeddings, nil
}

type bedrockProvider struct {
	config  *adk.EmbeddingConfig
	once    sync.Once
	client  *bedrockruntime.Client
	initErr error
}

func (p *bedrockProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	region := os.Getenv("AWS_DEFAULT_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}

	p.once.Do(func() {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			p.initErr = fmt.Errorf("failed to load AWS config: %w", err)
			return
		}
		p.client = bedrockruntime.NewFromConfig(awsCfg)
	})
	if p.initErr != nil {
		return nil, p.initErr
	}

	embeddings := make([][]float32, 0, len(texts))
	for i, text := range texts {
		reqBody, err := json.Marshal(map[string]string{"inputText": text})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request for text %d: %w", i, err)
		}
		output, err := p.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(p.config.Model),
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
			embedding = normalizeL2(embedding[:TargetDimension])
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
