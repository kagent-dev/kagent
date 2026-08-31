/**
 * Presentational metadata for each model provider — a display name, and the
 * pages an operator goes to for an API key or the list of available models.
 *
 * Keyed by the provider `type` the controller reports (which the backend uses as
 * both the enum and the display key). A provider the controller adds that is not
 * listed here still works; it simply shows its raw type and no help links.
 */
export interface ProviderInfo {
  /** Friendly name; falls back to the raw type when absent. */
  displayName: string;
  /** Where to get a key, or null for providers that need none (Ollama). */
  apiKeyLink: string | null;
  /** Documentation for the models this provider offers. */
  modelDocsLink?: string;
}

export const PROVIDER_INFO: Record<string, ProviderInfo> = {
  OpenAI: {
    displayName: "OpenAI",
    apiKeyLink: "https://platform.openai.com/settings/api-keys",
    modelDocsLink: "https://platform.openai.com/docs/models",
  },
  AzureOpenAI: {
    displayName: "Azure OpenAI",
    apiKeyLink: "https://portal.azure.com/",
    modelDocsLink:
      "https://learn.microsoft.com/azure/ai-services/openai/concepts/models",
  },
  Anthropic: {
    displayName: "Anthropic",
    apiKeyLink: "https://console.anthropic.com/settings/keys",
    modelDocsLink: "https://docs.anthropic.com/en/docs/about-claude/models",
  },
  Ollama: {
    displayName: "Ollama",
    apiKeyLink: null,
    modelDocsLink: "https://ollama.com/library",
  },
  Gemini: {
    displayName: "Gemini",
    apiKeyLink: "https://ai.google.dev/",
    modelDocsLink: "https://ai.google.dev/gemini-api/docs/models",
  },
  GeminiVertexAI: {
    displayName: "Gemini Vertex AI",
    apiKeyLink: "https://cloud.google.com/vertex-ai",
    modelDocsLink: "https://cloud.google.com/vertex-ai/generative-ai/docs/models",
  },
  AnthropicVertexAI: {
    displayName: "Anthropic Vertex AI",
    apiKeyLink: "https://cloud.google.com/vertex-ai",
    modelDocsLink:
      "https://docs.anthropic.com/en/api/claude-on-vertex-ai",
  },
  Bedrock: {
    displayName: "AWS Bedrock",
    apiKeyLink: "https://console.aws.amazon.com/bedrock/",
    modelDocsLink:
      "https://docs.aws.amazon.com/bedrock/latest/userguide/models-supported.html",
  },
  SAPAICore: {
    displayName: "SAP AI Core",
    apiKeyLink: "https://help.sap.com/docs/sap-ai-core",
    modelDocsLink:
      "https://help.sap.com/docs/sap-ai-core/sap-ai-core-service-guide/models-and-scenarios-in-generative-ai-hub",
  },
};

/** The provider's friendly name, or its raw type if unknown. */
export function providerDisplayName(type: string): string {
  return PROVIDER_INFO[type]?.displayName ?? type;
}

/** Ollama runs locally and needs no credential; nothing else is assumed keyless. */
export const OLLAMA_PROVIDER = "Ollama";

/**
 * Providers the CRD permits `apiKeyPassthrough` on — where the caller's own
 * credential is forwarded rather than one stored in the cluster. The passthrough
 * auth option is offered only for these.
 */
export const PASSTHROUGH_ALLOWED_PROVIDERS = new Set([
  "OpenAI",
  "Anthropic",
  "AzureOpenAI",
  "Ollama",
  "Bedrock",
  "SAPAICore",
]);

export function supportsPassthrough(providerType: string | undefined): boolean {
  return !!providerType && PASSTHROUGH_ALLOWED_PROVIDERS.has(providerType);
}

/** The default tag an Ollama model runs under when none is given. */
export const OLLAMA_DEFAULT_TAG = "latest";
