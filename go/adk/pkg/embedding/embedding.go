package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/ollama/ollama/api"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
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
}

// New creates a new embedding client.
func New(cfg Config) (*Client, error) {
	if cfg.EmbeddingConfig == nil {
		return nil, fmt.Errorf("embedding config is required")
	}
	if cfg.EmbeddingConfig.Model == "" {
		return nil, fmt.Errorf("embedding model is required")
	}
	p, err := newProvider(cfg.EmbeddingConfig)
	if err != nil {
		return nil, err
	}
	return &Client{
		config: cfg.EmbeddingConfig,
		p:      p,
	}, nil
}

func newProvider(cfg *adk.EmbeddingConfig) (provider, error) {
	switch cfg.Provider {
	case "azure_openai":
		return newAzureOpenAIProvider(cfg)
	case "ollama":
		return newOllamaProvider(cfg)
	case "gemini", "vertex_ai":
		return &geminiProvider{config: cfg}, nil
	case "bedrock":
		return &bedrockProvider{config: cfg}, nil
	case "foundry":
		return &foundryProvider{config: cfg, httpClient: defaultProviderHTTPClient()}, nil
	default: // "openai", "", and unknown providers
		return newOpenAIProvider(cfg)
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
	config *adk.EmbeddingConfig
	client openai.Client
}

func newOpenAIProvider(cfg *adk.EmbeddingConfig) (*openAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(defaultProviderHTTPClient()),
	}
	if cfg.BaseUrl != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseUrl))
	}
	return &openAIProvider{
		config: cfg,
		client: openai.NewClient(opts...),
	}, nil
}

func (p *openAIProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	resp, err := p.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model:      openai.EmbeddingModel(p.config.Model),
		Input:      openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Dimensions: openai.Int(int64(TargetDimension)),
	})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings request failed: %w", err)
	}

	raw := make([][]float32, len(resp.Data))
	for i, item := range resp.Data {
		raw[i] = float64ToFloat32(item.Embedding)
	}
	return processEmbeddings(log, raw, "openai")
}

type azureOpenAIProvider struct {
	config *adk.EmbeddingConfig
	client openai.Client
}

func newAzureOpenAIProvider(cfg *adk.EmbeddingConfig) (*azureOpenAIProvider, error) {
	apiVersion := os.Getenv("OPENAI_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}

	azureEndpoint := cfg.BaseUrl
	if azureEndpoint == "" {
		azureEndpoint = os.Getenv("AZURE_OPENAI_ENDPOINT")
	}
	if azureEndpoint == "" {
		return nil, fmt.Errorf("Azure OpenAI endpoint must be set via base_url or AZURE_OPENAI_ENDPOINT env var") //nolint:staticcheck // ST1005: keep product name readable
	}

	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	baseURL := strings.TrimSuffix(azureEndpoint, "/")
	if !strings.Contains(baseURL, "/openai/deployments/") {
		baseURL += "/openai/deployments/" + url.PathEscape(cfg.Model)
	}
	baseURL += "/"

	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithQueryAdd("api-version", apiVersion),
		option.WithHeader("Api-Key", apiKey),
		option.WithHTTPClient(defaultProviderHTTPClient()),
	}
	return &azureOpenAIProvider{
		config: cfg,
		client: openai.NewClient(opts...),
	}, nil
}

func (p *azureOpenAIProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	resp, err := p.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model:      openai.EmbeddingModel(p.config.Model),
		Input:      openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Dimensions: openai.Int(int64(TargetDimension)),
	})
	if err != nil {
		return nil, fmt.Errorf("azure openai embeddings request failed: %w", err)
	}

	raw := make([][]float32, len(resp.Data))
	for i, item := range resp.Data {
		raw[i] = float64ToFloat32(item.Embedding)
	}
	return processEmbeddings(log, raw, "azure_openai")
}

type ollamaProvider struct {
	config *adk.EmbeddingConfig
	client *api.Client
}

func newOllamaProvider(cfg *adk.EmbeddingConfig) (*ollamaProvider, error) {
	host := cfg.BaseUrl
	if host == "" {
		host = os.Getenv("OLLAMA_API_BASE")
	}
	if host == "" {
		host = "http://localhost:11434"
	}

	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama host URL %q: %w", host, err)
	}
	return &ollamaProvider{
		config: cfg,
		client: api.NewClient(baseURL, defaultProviderHTTPClient()),
	}, nil
}

func (p *ollamaProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	resp, err := p.client.Embed(ctx, &api.EmbedRequest{
		Model:      p.config.Model,
		Input:      texts,
		Dimensions: TargetDimension,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embed request failed: %w", err)
	}
	return processEmbeddings(log, resp.Embeddings, "ollama")
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
	raw := make([][]float32, len(texts))
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
			raw[i] = emb
		}
	}
	return processEmbeddings(log, raw, "gemini")
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

	raw := make([][]float32, 0, len(texts))
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
		raw = append(raw, result.Embedding)
	}
	return processEmbeddings(log, raw, "bedrock")
}

type bedrockEmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

func defaultProviderHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}

func processEmbeddings(log logr.Logger, embeddings [][]float32, provider string) ([][]float32, error) {
	result := make([][]float32, 0, len(embeddings))
	for _, embedding := range embeddings {
		if len(embedding) > TargetDimension {
			log.V(1).Info("Truncating embedding", "from", len(embedding), "to", TargetDimension)
			embedding = normalizeL2(embedding[:TargetDimension])
		} else if len(embedding) < TargetDimension {
			return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
		}
		result = append(result, embedding)
	}
	log.Info("Successfully generated embeddings", "provider", provider, "count", len(result))
	return result, nil
}

func float64ToFloat32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(x)
	}
	return out
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

// foundryCognitiveServicesScope is the Azure data-plane scope used to request
// tokens for the Foundry / Azure AI Services OpenAI-compatible API.
const foundryCognitiveServicesScope = "https://cognitiveservices.azure.com/.default"

type foundryTokenCredential interface {
	GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error)
}

// foundryCredentialFactory constructs the Azure credential for the implicit
// Workload Identity auth path. It is a package variable so tests can inject a
// fake credential.
var foundryCredentialFactory = func() (foundryTokenCredential, error) {
	return azidentity.NewDefaultAzureCredential(nil)
}

// foundryProvider generates embeddings through Azure AI Foundry's
// OpenAI-compatible data plane. Authentication is implicit and mirrors the
// Foundry chat model: FOUNDRY_API_KEY is used when set, otherwise the provider
// authenticates with DefaultAzureCredential (Azure Workload Identity in-cluster).
type foundryProvider struct {
	config     *adk.EmbeddingConfig
	httpClient *http.Client
	credential foundryTokenCredential
}

func (p *foundryProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logr.FromContextOrDiscard(ctx)

	endpoint := p.config.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("FOUNDRY_ENDPOINT")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required for Foundry embeddings")
	}
	deployment := p.config.Deployment
	if deployment == "" {
		deployment = p.config.Model
	}
	if deployment == "" {
		deployment = os.Getenv("FOUNDRY_DEPLOYMENT")
	}
	if deployment == "" {
		return nil, fmt.Errorf("deployment is required for Foundry embeddings")
	}
	apiVersion := p.config.APIVersion
	if apiVersion == "" {
		apiVersion = os.Getenv("FOUNDRY_API_VERSION")
	}
	if apiVersion == "" {
		apiVersion = "2024-10-21"
	}

	requestURL, err := foundryEmbeddingsURL(endpoint, deployment, apiVersion)
	if err != nil {
		return nil, err
	}
	reqBody := map[string]any{
		"input":      texts,
		"dimensions": TargetDimension,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Implicit auth: API key when set, otherwise a DefaultAzureCredential bearer token.
	if apiKey := os.Getenv("FOUNDRY_API_KEY"); apiKey != "" {
		req.Header.Set("Api-Key", apiKey)
	} else {
		credential := p.credential
		if credential == nil {
			credential, err = foundryCredentialFactory()
			if err != nil {
				return nil, fmt.Errorf("failed to create Azure credential for Foundry: %w", err)
			}
			p.credential = credential
		}
		token, err := credential.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{foundryCognitiveServicesScope}})
		if err != nil {
			return nil, fmt.Errorf("failed to acquire Foundry token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.Token)
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

	var result foundryEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	embeddings := make([][]float32, 0, len(result.Data))
	for _, item := range result.Data {
		embedding, err := adjustEmbeddingDimension(item.Embedding)
		if err != nil {
			return nil, err
		}
		if len(item.Embedding) > TargetDimension {
			log.V(1).Info("Truncating embedding", "from", len(item.Embedding), "to", TargetDimension)
		}
		embeddings = append(embeddings, embedding)
	}
	log.Info("Successfully generated embeddings with Foundry", "count", len(embeddings))
	return embeddings, nil
}

func foundryEmbeddingsURL(endpoint, deployment, apiVersion string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to parse Foundry endpoint: %w", err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/openai/deployments/" + url.PathEscape(deployment) + "/embeddings"
	query := parsed.Query()
	query.Set("api-version", apiVersion)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// foundryEmbeddingResponse models the OpenAI-compatible embeddings response
// returned by the Foundry data plane.
type foundryEmbeddingResponse struct {
	Data []foundryEmbeddingData `json:"data"`
}

type foundryEmbeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

func adjustEmbeddingDimension(embedding []float32) ([]float32, error) {
	if len(embedding) > TargetDimension {
		return normalizeL2(embedding[:TargetDimension]), nil
	}
	if len(embedding) < TargetDimension {
		return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
	}
	return embedding, nil
}
