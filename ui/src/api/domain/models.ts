/** ModelConfig domain models, mirroring the `kagent.dev/v1alpha2` ModelConfig CRD. */

import type { TLSConfig } from "./common";

export interface OpenAIConfig {
  baseUrl?: string;
  organization?: string;
  temperature?: string;
  maxTokens?: number;
  topP?: string;
  frequencyPenalty?: string;
  presencePenalty?: string;
  seed?: number;
  n?: number;
  timeout?: number;
  reasoningEffort?: string;
}

export interface AnthropicConfig {
  baseUrl?: string;
  maxTokens?: number;
  temperature?: string;
  topP?: string;
  topK?: number;
}

export interface AzureOpenAIConfig {
  azureEndpoint: string;
  apiVersion: string;
  azureDeployment?: string;
  azureAdToken?: string;
  temperature?: string;
  maxTokens?: number;
  topP?: string;
}

export interface OllamaConfig {
  host?: string;
  options?: Record<string, string>;
}

export interface GeminiConfig {
  baseUrl?: string;
  temperature?: string;
  maxTokens?: number;
  topP?: string;
  topK?: number;
}

export interface GeminiVertexAIConfig {
  projectID?: string;
  location?: string;
  temperature?: string;
  topP?: string;
  topK?: number;
  stopSequences?: string[];
  maxOutputTokens?: number;
  candidateCount?: number;
  responseMimeType?: string;
}

export interface AnthropicVertexAIConfig {
  projectID?: string;
  location?: string;
  temperature?: string;
  topP?: string;
  topK?: number;
  stopSequences?: string[];
  maxTokens?: number;
}

export interface BedrockConfig {
  region: string;
}

export interface SAPAICoreConfig {
  baseUrl: string;
  resourceGroup?: string;
  authUrl?: string;
}

export interface FoundryConfig {
  endpoint?: string;
  deployment: string;
  apiVersion?: string;
  /** OpenAI-compatible chat completions (default) or the Anthropic Messages API for Claude models. */
  apiFormat?: "OpenAI" | "Anthropic";
}

export interface ModelConfigSpec {
  model: string;
  provider: string;
  apiKeySecret?: string;
  apiKeySecretKey?: string;
  apiKeyPassthrough?: boolean;
  defaultHeaders?: Record<string, string>;
  tls?: TLSConfig;
  openAI?: OpenAIConfig;
  anthropic?: AnthropicConfig;
  azureOpenAI?: AzureOpenAIConfig;
  ollama?: OllamaConfig;
  gemini?: GeminiConfig;
  geminiVertexAI?: GeminiVertexAIConfig;
  anthropicVertexAI?: AnthropicVertexAIConfig;
  bedrock?: BedrockConfig;
  sapAICore?: SAPAICoreConfig;
  foundry?: FoundryConfig;
}

/** One row of `ModelService.ListModelConfigs`: `ref` is `namespace/name`. */
export interface ModelConfig {
  ref: string;
  spec: ModelConfigSpec;
}

/** A model provider the backend knows how to configure. */
export interface Provider {
  name: string;
  type: string;
  requiredParams: string[];
  optionalParams: string[];
  /** Stock providers ship with the controller; configured ones were added by a user. */
  source?: "stock" | "configured";
  endpoint?: string;
}

export interface ProviderModel {
  name: string;
  function_calling: boolean;
}

/**
 * Models on offer, grouped by provider name.
 *
 * Built from `ModelService.ListSupportedModels`, which answers with one entry per
 * provider; the record shape is kept because every picker in the app indexes it
 * by provider.
 */
export type ProviderModelsResponse = Record<string, ProviderModel[]>;

export interface SecretMaterial {
  name: string;
  key: string;
  value: string;
}

export interface CreateModelConfigRequest {
  ref: string;
  apiKey?: string;
  spec: ModelConfigSpec;
  secrets?: SecretMaterial[];
}

export interface UpdateModelConfigRequest {
  apiKey?: string | null;
  spec: ModelConfigSpec;
  secrets?: SecretMaterial[];
}
