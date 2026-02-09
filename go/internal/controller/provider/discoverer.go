/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultTimeout is the default HTTP timeout for model discovery requests
	DefaultTimeout = 30 * time.Second
)

// ModelDiscoverer fetches available models from LLM provider APIs.
// It supports OpenAI-compatible APIs and provider-specific endpoints.
type ModelDiscoverer struct {
	httpClient *http.Client
}

// NewModelDiscoverer creates a new ModelDiscoverer instance.
func NewModelDiscoverer() *ModelDiscoverer {
	return &ModelDiscoverer{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// openAIModelsResponse represents the response from OpenAI-compatible /models endpoints.
// This format is used by OpenAI, Anthropic, and most other providers.
type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// DiscoverModels calls the provider's models endpoint and returns available model IDs.
// Most providers use OpenAI-compatible /v1/models, but auth headers vary by provider.
func (d *ModelDiscoverer) DiscoverModels(ctx context.Context, providerType v1alpha2.ModelProvider, endpoint, apiKey string) ([]string, error) {
	logger := log.FromContext(ctx).WithName("model-discoverer")

	// Ollama has a completely different API - delegate to specialized function
	if providerType == v1alpha2.ModelProviderOllama {
		logger.V(1).Info("Discovering models from Ollama", "endpoint", endpoint)
		return d.DiscoverOllamaModels(ctx, endpoint)
	}

	modelsURL := buildModelsURL(endpoint, providerType)
	logger.V(1).Info("Discovering models", "provider", providerType, "url", modelsURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers based on provider type
	d.setAuthHeaders(req, providerType, apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle error responses
	switch resp.StatusCode {
	case http.StatusOK:
		return d.parseModelsResponse(resp)
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("unauthorized: invalid API key for provider %s", providerType)
	case http.StatusForbidden:
		return nil, fmt.Errorf("forbidden: API key lacks permission to list models for provider %s", providerType)
	case http.StatusNotFound:
		return nil, fmt.Errorf("models endpoint not found for provider %s (URL: %s)", providerType, modelsURL)
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d for provider %s: %s", resp.StatusCode, providerType, string(body))
	}
}

// setAuthHeaders sets the appropriate authentication headers based on provider type.
func (d *ModelDiscoverer) setAuthHeaders(req *http.Request, providerType v1alpha2.ModelProvider, apiKey string) {
	switch providerType {
	case v1alpha2.ModelProviderAnthropic, v1alpha2.ModelProviderAnthropicVertexAI:
		// Anthropic uses x-api-key header
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case v1alpha2.ModelProviderGemini, v1alpha2.ModelProviderGeminiVertexAI:
		// Google uses query parameter for API key (handled in URL) or Bearer token
		req.Header.Set("Authorization", "Bearer "+apiKey)
	default:
		// OpenAI and compatible providers use Bearer token
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// parseModelsResponse parses OpenAI-compatible response and extracts model IDs.
// Format: {"data": [{"id": "model-name"}, ...]}
func (d *ModelDiscoverer) parseModelsResponse(resp *http.Response) ([]string, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result openAIModelsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}

	return models, nil
}

// buildModelsURL constructs the models endpoint URL based on provider type.
// Note: Ollama is handled separately via DiscoverOllamaModels.
func buildModelsURL(endpoint string, providerType v1alpha2.ModelProvider) string {
	endpoint = strings.TrimSuffix(endpoint, "/")

	switch providerType {
	case v1alpha2.ModelProviderAnthropic:
		// Anthropic: https://api.anthropic.com/v1/models
		if strings.HasSuffix(endpoint, "/v1") {
			return endpoint + "/models"
		}
		return endpoint + "/v1/models"

	case v1alpha2.ModelProviderGemini:
		// Google AI: https://generativelanguage.googleapis.com/v1beta/models
		if strings.Contains(endpoint, "generativelanguage.googleapis.com") {
			return endpoint + "/v1beta/models"
		}
		return endpoint + "/v1/models"

	case v1alpha2.ModelProviderGeminiVertexAI, v1alpha2.ModelProviderAnthropicVertexAI:
		// Vertex AI has different discovery patterns - may not be supported
		return endpoint + "/v1/models"

	default:
		// OpenAI and compatible (Azure OpenAI, LiteLLM, vLLM, etc.)
		if strings.HasSuffix(endpoint, "/v1") {
			return endpoint + "/models"
		}
		return endpoint + "/v1/models"
	}
}

// OllamaTagsResponse represents Ollama's /api/tags response format
type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// DiscoverOllamaModels handles Ollama's different response format.
func (d *ModelDiscoverer) DiscoverOllamaModels(ctx context.Context, endpoint string) ([]string, error) {
	endpoint = strings.TrimSuffix(endpoint, "/")
	modelsURL := endpoint + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API returned status %d", resp.StatusCode)
	}

	var result ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse Ollama response: %w", err)
	}

	models := make([]string, 0, len(result.Models))
	for _, m := range result.Models {
		if m.Name != "" {
			models = append(models, m.Name)
		}
	}

	return models, nil
}
