import { Component, RunStatus, ToolConfig } from "@/types/datamodel";

export interface CreateAgentFormData {
  name: string;
  description: string;
  system_prompt: string;
  model: Model;
  tools: Component<ToolConfig>[];
}

export interface Model {
  name: string;
  namespace: string;
  providerName: string;
  model: string;
  apiKeySecretName: string;
  apiKeySecretKey: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  modelParams?: Record<string, any>; // Optional model-specific parameters
}

export interface CreateSessionRequest {
  name?: string;
  user_id: string;
  team_id: string;
}

export interface CreateRunRequest {
  user_id: string;
  session_id?: number;
}

export interface CreateRunResponse {
  run_id: string;
  status: RunStatus;
}

export interface BaseResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

export interface TokenStats {
  total: number;
  input: number;
  output: number;
}

export interface Provider {
  name: string;
  type: string;
  requiredParams: string[];
  optionalParams: string[];
}

interface OpenAIConfigPayload {
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
}

interface AnthropicConfigPayload {
    baseUrl?: string;
    maxTokens?: number;
    temperature?: string;
    topP?: string;
    topK?: number;
}

interface AzureOpenAIConfigPayload {
    azureEndpoint: string
    apiVersion: string;
    azureDeployment?: string;
    azureAdToken?: string;
    temperature?: string;
    maxTokens?: number;
    topP?: string;
}

interface OllamaConfigPayload {
    host?: string;
    options?: Record<string, string>;
}


export interface CreateModelConfigPayload {
  name: string;
  provider: Pick<Provider, "name" | "type">;
  model: string;
  apiKey: string;
  openAI?: OpenAIConfigPayload;
  anthropic?: AnthropicConfigPayload;
  azureOpenAI?: AzureOpenAIConfigPayload;
  ollama?: OllamaConfigPayload;
}

export interface UpdateModelConfigPayload {
    provider: Pick<Provider, "name" | "type">;
    model: string;
    apiKey?: string | null;
    openAI?: OpenAIConfigPayload;
    anthropic?: AnthropicConfigPayload;
    azureOpenAI?: AzureOpenAIConfigPayload;
    ollama?: OllamaConfigPayload;
}