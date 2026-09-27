package env

// LLM provider environment variables consumed by runtimes and provider SDKs.
// Managed runtimes receive configuration from the controller; credentials may
// instead be injected into requests at the egress gateway.

// OpenAI
var (
	OpenAIAPIKey = RegisterStringVar(
		"OPENAI_API_KEY",
		"",
		"API key for OpenAI. Upgrade tests fall back to a placeholder when unset or empty.",
		ComponentAgentRuntime, ComponentCLI, ComponentTesting,
	)

	OpenAIOrganization = RegisterStringVar(
		"OPENAI_ORGANIZATION",
		"",
		"OpenAI organization identifier.",
		ComponentAgentRuntime,
	)

	OpenAIAPIBase = RegisterStringVar(
		"OPENAI_API_BASE",
		"",
		"Custom base URL for the OpenAI API.",
		ComponentAgentRuntime,
	)
)

// Anthropic
var (
	AnthropicAPIKey = RegisterStringVar(
		"ANTHROPIC_API_KEY",
		"",
		"API key for Anthropic.",
		ComponentAgentRuntime, ComponentCLI,
	)
)

// Azure OpenAI
var (
	AzureOpenAIAPIKey = RegisterStringVar(
		"AZURE_OPENAI_API_KEY",
		"",
		"API key for Azure OpenAI.",
		ComponentAgentRuntime, ComponentCLI,
	)

	AzureADToken = RegisterStringVar(
		"AZURE_AD_TOKEN",
		"",
		"Azure Active Directory authentication token for Azure OpenAI.",
		ComponentAgentRuntime,
	)

	OpenAIAPIVersion = RegisterStringVar(
		"OPENAI_API_VERSION",
		"",
		"Azure OpenAI API version. The Go and Python ADKs fall back to 2024-02-15-preview when model configuration and this variable are unset.",
		ComponentAgentRuntime,
	)

	AzureOpenAIEndpoint = RegisterStringVar(
		"AZURE_OPENAI_ENDPOINT",
		"",
		"Endpoint URL for Azure OpenAI service.",
		ComponentAgentRuntime,
	)
)

// Google Cloud / Gemini / Vertex AI
var (
	GoogleAPIKey = RegisterStringVar(
		"GOOGLE_API_KEY",
		"",
		"API key for Google Gemini.",
		ComponentAgentRuntime, ComponentCLI,
	)

	GoogleCloudProject = RegisterStringVar(
		"GOOGLE_CLOUD_PROJECT",
		"",
		"Google Cloud project ID for Vertex AI.",
		ComponentAgentRuntime,
	)

	GoogleCloudLocation = RegisterStringVar(
		"GOOGLE_CLOUD_LOCATION",
		"",
		"Google Cloud region/location for Vertex AI.",
		ComponentAgentRuntime,
	)

	GoogleGenAIUseVertexAI = RegisterStringVar(
		"GOOGLE_GENAI_USE_VERTEXAI",
		"",
		"When set to 'true', use Vertex AI for Gemini models.",
		ComponentAgentRuntime,
	)

	GoogleApplicationCredentials = RegisterStringVar(
		"GOOGLE_APPLICATION_CREDENTIALS",
		"",
		"Path to Google Cloud service account JSON key file.",
		ComponentAgentRuntime,
	)
)

// AWS / Bedrock
var (
	AWSRegion = RegisterStringVar(
		"AWS_REGION",
		"",
		"AWS region for Bedrock. Python Bedrock and Go Bedrock embeddings prefer AWS_DEFAULT_REGION, then AWS_REGION, then us-east-1.",
		ComponentAgentRuntime,
	)

	AWSAccessKeyID = RegisterStringVar(
		"AWS_ACCESS_KEY_ID",
		"",
		"AWS access key ID for IAM authentication with Bedrock.",
		ComponentAgentRuntime,
	)

	AWSSecretAccessKey = RegisterStringVar(
		"AWS_SECRET_ACCESS_KEY",
		"",
		"AWS secret access key for IAM authentication with Bedrock.",
		ComponentAgentRuntime,
	)

	AWSSessionToken = RegisterStringVar(
		"AWS_SESSION_TOKEN",
		"",
		"AWS session token for temporary/SSO credentials with Bedrock.",
		ComponentAgentRuntime,
	)

	AWSBearerTokenBedrock = RegisterStringVar(
		"AWS_BEARER_TOKEN_BEDROCK",
		"",
		"Bearer token for authentication with AWS Bedrock.",
		ComponentAgentRuntime,
	)
)

// Ollama
var (
	OllamaAPIBase = RegisterStringVar(
		"OLLAMA_API_BASE",
		"",
		"Base URL for the Ollama API endpoint; falls back to http://localhost:11434 when model configuration and this variable are unset.",
		ComponentAgentRuntime,
	)

	OllamaAPIKey = RegisterStringVar(
		"OLLAMA_API_KEY",
		"",
		"API key for Ollama Cloud. When set, a cloud-tagged model reaches api.ollama.com directly.",
		ComponentAgentRuntime, ComponentCLI,
	)
)

// Provider aliases.
var (
	GeminiAPIKey      = RegisterStringVar("GEMINI_API_KEY", "", "Fallback Gemini API key when GOOGLE_API_KEY is unset; supported by the CLI and Go/Python ADKs.", ComponentAgentRuntime, ComponentCLI)
	GoogleCloudRegion = RegisterStringVar("GOOGLE_CLOUD_REGION", "", "Go ADK Vertex AI region fallback when GOOGLE_CLOUD_LOCATION is unset.", ComponentAgentRuntime)
	AWSDefaultRegion  = RegisterStringVar("AWS_DEFAULT_REGION", "", "Preferred region for Python Bedrock models and Go/Python Bedrock embeddings, before AWS_REGION and the us-east-1 fallback.", ComponentAgentRuntime)
)

// SAP AI Core
var (
	SAPAICoreClientID = RegisterStringVar(
		"SAP_AI_CORE_CLIENT_ID",
		"",
		"OAuth2 client ID for SAP AI Core authentication.",
		ComponentAgentRuntime,
	)

	SAPAICoreClientSecret = RegisterStringVar(
		"SAP_AI_CORE_CLIENT_SECRET",
		"",
		"OAuth2 client secret for SAP AI Core authentication.",
		ComponentAgentRuntime,
	)
)

// Mistral
var (
	MistralAPIKey = RegisterStringVar(
		"MISTRAL_API_KEY",
		"",
		"API key for Mistral AI.",
		ComponentAgentRuntime,
	)

	MistralAPIBase = RegisterStringVar(
		"MISTRAL_API_BASE",
		"",
		"Custom base URL for the Mistral AI API (defaults to https://api.mistral.ai/v1).",
		ComponentAgentRuntime,
	)
)

// Foundry
var (
	FoundryAPIKey = RegisterStringVar(
		"FOUNDRY_API_KEY",
		"",
		"API key for Azure AI Foundry.",
		ComponentAgentRuntime,
	)

	FoundryEndpoint = RegisterStringVar(
		"FOUNDRY_ENDPOINT",
		"",
		"Endpoint URL for Azure AI Foundry or an Azure AI Services account.",
		ComponentAgentRuntime,
	)

	FoundryDeployment = RegisterStringVar(
		"FOUNDRY_DEPLOYMENT",
		"",
		"Azure AI Foundry model deployment name.",
		ComponentAgentRuntime,
	)

	FoundryAPIVersion = RegisterStringVar(
		"FOUNDRY_API_VERSION",
		"2024-10-21",
		"Azure AI Foundry OpenAI-compatible data-plane API version.",
		ComponentAgentRuntime,
	)
)
