package api

// Model Configurations
type ModelInfo struct {
	Vision          bool   `json:"vision"`
	FunctionCalling bool   `json:"function_calling"`
	JSONOutput      bool   `json:"json_output"`
	Family          string `json:"family"`
}

type CreateArgumentsConfig struct {
	FrequencyPenalty *float64           `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]float64 `json:"logit_bias,omitempty"`
	MaxTokens        *int               `json:"max_tokens,omitempty"`
	N                *int               `json:"n,omitempty"`
	PresencePenalty  *float64           `json:"presence_penalty,omitempty"`
	ResponseFormat   interface{}        `json:"response_format,omitempty"`
	Seed             *int               `json:"seed,omitempty"`
	Stop             interface{}        `json:"stop,omitempty"`
	Temperature      *float64           `json:"temperature,omitempty"`
	TopP             *float64           `json:"top_p,omitempty"`
	User             *string            `json:"user,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type BaseOpenAIClientConfig struct {
	// Base OpenAI fields
	Model             string         `json:"model"`
	APIKey            *string        `json:"api_key,omitempty"`
	Timeout           *int           `json:"timeout,omitempty"`
	MaxRetries        *int           `json:"max_retries,omitempty"`
	ModelCapabilities interface{}    `json:"model_capabilities,omitempty"`
	ModelInfo         *ModelInfo     `json:"model_info,omitempty"`
	StreamOptions     *StreamOptions `json:"stream_options,omitempty"`
	CreateArgumentsConfig
}

type OpenAIClientConfig struct {
	BaseOpenAIClientConfig

	// OpenAIClientConfig specific fields
	Organization *string `json:"organization,omitempty"`
	BaseURL      *string `json:"base_url,omitempty"`
}

func (c *OpenAIClientConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *OpenAIClientConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type AzureOpenAIClientConfig struct {
	BaseOpenAIClientConfig

	// AzureOpenAIClientConfig specific fields
	AzureEndpoint        *string     `json:"azure_endpoint,omitempty"`
	AzureDeployment      *string     `json:"azure_deployment,omitempty"`
	APIVersion           *string     `json:"api_version,omitempty"`
	AzureADToken         *string     `json:"azure_ad_token,omitempty"`
	AzureADTokenProvider interface{} `json:"azure_ad_token_provider,omitempty"`
}

func (c *AzureOpenAIClientConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *AzureOpenAIClientConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}
