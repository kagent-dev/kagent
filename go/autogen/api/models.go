package api

// Model Configurations
type ModelInfo struct {
	Vision                 bool   `json:"vision"`
	FunctionCalling        bool   `json:"function_calling"`
	JSONOutput             bool   `json:"json_output"`
	Family                 string `json:"family"`
	StructuredOutput       bool   `json:"structured_output"`
	MultipleSystemMessages bool   `json:"multiple_system_messages"`
}

type OpenAICreateArgumentsConfig struct {
	FrequencyPenalty float64            `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]float64 `json:"logit_bias,omitempty"`
	MaxTokens        int                `json:"max_tokens,omitempty"`
	N                int                `json:"n,omitempty"`
	PresencePenalty  float64            `json:"presence_penalty,omitempty"`
	Seed             int                `json:"seed,omitempty"`
	Temperature      float64            `json:"temperature,omitempty"`
	TopP             float64            `json:"top_p,omitempty"`
	User             string             `json:"user,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type BaseOpenAIClientConfig struct {
	// Base OpenAI fields
	Model             string         `json:"model"`
	APIKey            string         `json:"api_key,omitempty"`
	Timeout           int            `json:"timeout,omitempty"`
	MaxRetries        int            `json:"max_retries,omitempty"`
	ModelCapabilities interface{}    `json:"model_capabilities,omitempty"`
	ModelInfo         *ModelInfo     `json:"model_info,omitempty"`
	StreamOptions     *StreamOptions `json:"stream_options,omitempty"`
	OpenAICreateArgumentsConfig
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
	AzureEndpoint   string `json:"azure_endpoint,omitempty"`
	AzureDeployment string `json:"azure_deployment,omitempty"`
	APIVersion      string `json:"api_version,omitempty"`
	AzureADToken    string `json:"azure_ad_token,omitempty"`
}

func (c *AzureOpenAIClientConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *AzureOpenAIClientConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type AnthropicCreateArguments struct {
	MaxTokens     int               `json:"max_tokens,omitempty"`
	Temperature   float64           `json:"temperature,omitempty"`
	TopP          float64           `json:"top_p,omitempty"`
	TopK          int               `json:"top_k,omitempty"`
	StopSequences []string          `json:"stop_sequences,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type BaseAnthropicClientConfiguration struct {
	APIKey            string            `json:"api_key,omitempty"`
	BaseURL           string            `json:"base_url,omitempty"`
	Model             string            `json:"model"`
	ModelCapabilities *ModelInfo        `json:"model_capabilities,omitempty"`
	ModelInfo         *ModelInfo        `json:"model_info,omitempty"`
	Timeout           float64           `json:"timeout,omitempty"`
	MaxRetries        int               `json:"max_retries,omitempty"`
	DefaultHeaders    map[string]string `json:"default_headers,omitempty"`
	AnthropicCreateArguments
}

type AnthropicClientConfiguration struct {
	BaseAnthropicClientConfiguration
}

func (c *AnthropicClientConfiguration) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *AnthropicClientConfiguration) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type OllamaCreateArguments struct {
	Model string `json:"model"`
	Host  string `json:"host"`
}

type OllamaClientConfiguration struct {
	FollowRedirects   bool              `json:"follow_redirects"`
	Timeout           int               `json:"timeout"`
	Headers           map[string]string `json:"headers"`
	ModelCapabilities interface{}       `json:"model_capabilities,omitempty"`
	ModelInfo         *ModelInfo        `json:"model_info"`
	Options           map[string]string `json:"options"`
	OllamaCreateArguments
}

func (c *OllamaClientConfiguration) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *OllamaClientConfiguration) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}
