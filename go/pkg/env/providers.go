package env

// LLM provider environment variables. These are injected into agent pods
// by the controller and consumed by the agent runtime.

// OpenAI
var (
	OpenAIAPIKey = RegisterStringVar(
		"OPENAI_API_KEY",
		"",
		"API key for OpenAI.",
		ComponentAgentRuntime,
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
		ComponentAgentRuntime,
	)
)

// Azure OpenAI
var (
	AzureOpenAIAPIKey = RegisterStringVar(
		"AZURE_OPENAI_API_KEY",
		"",
		"API key for Azure OpenAI.",
		ComponentAgentRuntime,
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
		"Azure OpenAI API version (e.g. 2024-02-15-preview).",
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
		ComponentAgentRuntime,
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
		"AWS region for Bedrock.",
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
		"Base URL for the Ollama API endpoint.",
		ComponentAgentRuntime,
	)
)
