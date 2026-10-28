package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/ollama/ollama/api"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/internal/azureai"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
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
	config *configv1.ADKEmbeddingConfig
	p      provider
}

// Config for creating an embedding client.
type Config struct {
	EmbeddingConfig *configv1.ADKEmbeddingConfig
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

func newProvider(cfg *configv1.ADKEmbeddingConfig) (provider, error) {
	switch cfg.Provider {
	case "azure_openai":
		return newAzureOpenAIProvider(cfg, nil)
	case "ollama":
		return newOllamaProvider(cfg)
	case "gemini", "vertex_ai":
		return &geminiProvider{config: cfg}, nil
	case "bedrock":
		return &bedrockProvider{config: cfg}, nil
	case "foundry":
		return newFoundryProvider(cfg, nil)
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
	logging.FromContext(ctx).DebugContext(ctx, "generating embeddings", "count", len(texts), "model", c.config.Model)
	return c.p.generate(ctx, texts)
}

type openAIProvider struct {
	config *configv1.ADKEmbeddingConfig
	client openai.Client
}

func newOpenAIProvider(cfg *configv1.ADKEmbeddingConfig) (*openAIProvider, error) {
	apiKey := env.OpenAIAPIKey.Get()
	httpClient, err := embeddingHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	}
	if cfg.BaseUrl != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseUrl))
	}
	return &openAIProvider{
		config: cfg,
		client: openai.NewClient(opts...),
	}, nil
}

func generateEmbeddings(ctx context.Context, client openai.Client, cfg *configv1.ADKEmbeddingConfig, provider string, isAzureFamily bool, texts []string) ([][]float32, error) {
	log := logging.FromContext(ctx)

	resp, err := client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model:      openai.EmbeddingModel(cfg.Model),
		Input:      openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Dimensions: openai.Int(int64(TargetDimension)),
	}, embeddingPassthroughOpts(ctx, cfg, isAzureFamily)...)
	if err != nil {
		return nil, fmt.Errorf("%s embeddings request failed: %w", provider, err)
	}

	raw := make([][]float32, len(resp.Data))
	for i, item := range resp.Data {
		raw[i] = float64ToFloat32(item.Embedding)
	}
	return processEmbeddings(log, raw, provider)
}

// embeddingPassthroughOpts uses models.PassthroughToken so the embedding client
// resolves API key passthrough the same way as the chat/completions clients in
// go/adk/pkg/models.
func embeddingPassthroughOpts(ctx context.Context, cfg *configv1.ADKEmbeddingConfig, isAzureFamily bool) []option.RequestOption {
	if cfg == nil {
		return nil
	}
	token, ok := models.PassthroughToken(ctx, cfg.ApiKeyPassthrough)
	if !ok {
		return nil
	}
	if isAzureFamily {
		return []option.RequestOption{option.WithHeader("Api-Key", token)}
	}
	return []option.RequestOption{option.WithAPIKey(token)}
}

func (p *openAIProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	return generateEmbeddings(ctx, p.client, p.config, "openai", false, texts)
}

type azureOpenAIProvider struct {
	config *configv1.ADKEmbeddingConfig
	client openai.Client
}

// newAzureOpenAIProvider builds an Azure OpenAI embedding provider. Tests can
// inject a credential with cred; a nil cred uses the default Azure credential.
func newAzureOpenAIProvider(cfg *configv1.ADKEmbeddingConfig, cred azureai.TokenCredential) (*azureOpenAIProvider, error) {
	apiVersion := cfg.ApiVersion
	if apiVersion == "" {
		apiVersion = env.OpenAIAPIVersion.Get()
	}
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = cfg.BaseUrl
	}
	if endpoint == "" {
		endpoint = env.AzureOpenAIEndpoint.Get()
	}
	if endpoint == "" {
		return nil, fmt.Errorf("Azure OpenAI endpoint must be set via endpoint, base_url, or AZURE_OPENAI_ENDPOINT env var") //nolint:staticcheck // ST1005: keep product name readable
	}

	deployment := cfg.Deployment
	if deployment == "" {
		deployment = cfg.Model
	}
	httpClient, err := embeddingHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	clientCfg := azureai.ClientConfig{
		Endpoint:   endpoint,
		Deployment: deployment,
		APIVersion: apiVersion,
		HTTPClient: httpClient,
	}
	// Implicit auth mirrors NewAzureOpenAIModel: the incoming bearer
	// token when APIKeyPassthrough is enabled (a placeholder Api-Key is
	// overwritten per request by embeddingPassthroughOpts), otherwise the
	// AZURE_OPENAI_API_KEY Api-Key header, otherwise DefaultAzureCredential.
	apiKey := env.AzureOpenAIAPIKey.Get()
	if cfg.ApiKeyPassthrough {
		apiKey = "passthrough"
	}
	if err := azureai.ApplyImplicitAuth(context.Background(), &clientCfg, azureai.AuthOptions{
		APIKey:     apiKey,
		Credential: cred,
	}); err != nil {
		return nil, err
	}

	client, err := azureai.NewOpenAIClient(clientCfg)
	if err != nil {
		return nil, err
	}
	return &azureOpenAIProvider{
		config: cfg,
		client: client,
	}, nil
}

func (p *azureOpenAIProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	return generateEmbeddings(ctx, p.client, p.config, "azure_openai", true, texts)
}

type ollamaProvider struct {
	config *configv1.ADKEmbeddingConfig
	client *api.Client
}

func newOllamaProvider(cfg *configv1.ADKEmbeddingConfig) (*ollamaProvider, error) {
	host := cfg.BaseUrl
	if host == "" {
		host = env.OllamaAPIBase.Get()
	}
	if host == "" {
		host = "http://localhost:11434"
	}

	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama host URL %q: %w", host, err)
	}
	httpClient, err := embeddingHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return &ollamaProvider{
		config: cfg,
		client: api.NewClient(baseURL, httpClient),
	}, nil
}

func (p *ollamaProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logging.FromContext(ctx)

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
	config  *configv1.ADKEmbeddingConfig
	once    sync.Once
	client  *genai.Client
	initErr error
}

func (p *geminiProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logging.FromContext(ctx)

	p.once.Do(func() {
		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey: env.GoogleAPIKey.Get(),
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
	config  *configv1.ADKEmbeddingConfig
	once    sync.Once
	client  *bedrockruntime.Client
	initErr error
}

func (p *bedrockProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	log := logging.FromContext(ctx)

	region := env.AWSDefaultRegion.Get()
	if region == "" {
		region = env.AWSRegion.Get()
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

func embeddingHTTPClient(cfg *configv1.ADKEmbeddingConfig) (*http.Client, error) {
	timeout := 5 * time.Minute
	return models.BuildHTTPClient(models.TransportConfig{
		TLSInsecureSkipVerify: cfg.TlsInsecureSkipVerify,
		TLSCACertPath:         cfg.TlsCaCertPath,
		TLSDisableSystemCAs:   cfg.TlsDisableSystemCas,
		Timeout:               &timeout,
	})
}

func processEmbeddings(log *slog.Logger, embeddings [][]float32, provider string) ([][]float32, error) {
	result := make([][]float32, 0, len(embeddings))
	for _, embedding := range embeddings {
		if len(embedding) > TargetDimension {
			log.Debug("truncating embedding", "from", len(embedding), "to", TargetDimension)
			embedding = normalizeL2(embedding[:TargetDimension])
		} else if len(embedding) < TargetDimension {
			return nil, fmt.Errorf("embedding dimension %d is less than required %d", len(embedding), TargetDimension)
		}
		result = append(result, embedding)
	}
	log.Info("successfully generated embeddings", "provider", provider, "count", len(result))
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

// foundryProvider generates embeddings through Azure AI Foundry's
// OpenAI-compatible data plane using the shared azureai client. Authentication
// is implicit and mirrors the Foundry chat model: FOUNDRY_API_KEY is used when
// set, otherwise the provider authenticates with DefaultAzureCredential (Azure
// Workload Identity in-cluster).
type foundryProvider struct {
	config *configv1.ADKEmbeddingConfig
	client openai.Client
}

// newFoundryProvider builds a Foundry embedding provider. Tests can inject a
// credential with cred; a nil cred uses the default Azure credential.
func newFoundryProvider(cfg *configv1.ADKEmbeddingConfig, cred azureai.TokenCredential) (*foundryProvider, error) {
	deployment := cfg.Deployment
	if deployment == "" {
		deployment = cfg.Model
	}
	endpoint, deployment, apiVersion := azureai.ResolveFoundry(cfg.Endpoint, deployment, cfg.ApiVersion)
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required for Foundry embeddings")
	}
	if deployment == "" {
		return nil, fmt.Errorf("deployment is required for Foundry embeddings")
	}
	httpClient, err := embeddingHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	clientCfg := azureai.ClientConfig{
		Endpoint:   endpoint,
		Deployment: deployment,
		APIVersion: apiVersion,
		HTTPClient: httpClient,
	}
	// See newAzureOpenAIProvider - the passthrough placeholder short-circuits
	// past DefaultAzureCredential resolution the same way it does for chat.
	apiKey := env.FoundryAPIKey.Get()
	if cfg.ApiKeyPassthrough {
		apiKey = "passthrough"
	}
	if err := azureai.ApplyImplicitAuth(context.Background(), &clientCfg, azureai.AuthOptions{
		APIKey:     apiKey,
		Credential: cred,
	}); err != nil {
		return nil, err
	}

	client, err := azureai.NewOpenAIClient(clientCfg)
	if err != nil {
		return nil, err
	}
	return &foundryProvider{config: cfg, client: client}, nil
}

func (p *foundryProvider) generate(ctx context.Context, texts []string) ([][]float32, error) {
	return generateEmbeddings(ctx, p.client, p.config, "foundry", true, texts)
}
