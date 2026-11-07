package models

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// GeminiConfig configures the native Gemini API client.
type GeminiConfig struct {
	TransportConfig
	Model   string
	BaseURL string
	APIKey  string
}

const (
	geminiAPIKeyHeader          = "x-goog-api-key"
	geminiCredentialPlaceholder = "kagent-gemini-placeholder"
)

// NewGeminiModel supports custom endpoints with independently configured authentication.
func NewGeminiModel(ctx context.Context, config GeminiConfig) (model.LLM, error) {
	client, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build Gemini HTTP client: %w", err)
	}
	options := &genai.ClientConfig{Backend: genai.BackendGeminiAPI, APIKey: config.APIKey, HTTPClient: client}
	for header := range config.Headers {
		if strings.EqualFold(header, "authorization") {
			// Bearer authentication replaces API-key authentication, including
			// when the endpoint is reached through an outbound proxy.
			options.APIKey = geminiCredentialPlaceholder
			break
		}
	}
	if options.APIKey == "" {
		if config.BaseURL == "" {
			return nil, fmt.Errorf("gemini model requires GOOGLE_API_KEY or GEMINI_API_KEY environment variable")
		}
		options.APIKey = geminiCredentialPlaceholder
	}
	client.Transport = &geminiCredentialTransport{base: client.Transport}
	if config.BaseURL != "" {
		// An empty APIVersion selects v1beta. A dot is removed by the SDK's
		// URL.JoinPath, leaving the operator's complete API prefix unchanged.
		options.HTTPOptions = genai.HTTPOptions{BaseURL: config.BaseURL, APIVersion: "."}
	}
	llm, err := gemini.NewModel(ctx, config.Model, options)
	if err != nil {
		return nil, err
	}
	if config.BaseURL != "" {
		return &customGeminiModel{LLM: llm}, nil
	}
	return llm, nil
}

type customGeminiModel struct {
	model.LLM
}

func (m *customGeminiModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	request := *req
	name := request.Model
	if name == "" {
		name = m.Name()
	}
	// Custom endpoints treat the model ID as one URL path segment, not a
	// resource path. Escape it only for the SDK call, preserving the caller's ID.
	// A colon must also be escaped because it separates the operation suffix.
	request.Model = strings.ReplaceAll(url.PathEscape(name), ":", "%3A")
	return m.LLM.GenerateContent(ctx, &request, stream)
}

type geminiCredentialTransport struct {
	base http.RoundTripper
}

var _ http.RoundTripper = (*geminiCredentialTransport)(nil)

func (g *geminiCredentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Remove only the SDK initialization key, preserving explicitly configured
	// credential headers for the custom endpoint.
	if req.Header.Get(geminiAPIKeyHeader) == geminiCredentialPlaceholder {
		req = req.Clone(req.Context())
		req.Header.Del(geminiAPIKeyHeader)
	}
	return g.base.RoundTrip(req)
}
