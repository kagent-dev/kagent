export interface ModelConfig {
  ref: string;
  providerName: string;
  model: string;
  apiKeySecretRef: string;
  apiKeySecretKey: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  modelParams?: Record<string, any>; // Optional model-specific parameters
}

export interface CreateSessionRequest {
  agent_ref?: string;
  name?: string;
  user_id: string;
  id?: string;
}

export interface BaseResponse<T> {
  message: string;
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

// Export OpenAIConfigPayload
export interface OpenAIConfigPayload {
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

export interface AnthropicConfigPayload {
    baseUrl?: string;
    maxTokens?: number;
    temperature?: string;
    topP?: string;
    topK?: number;
}

export interface AzureOpenAIConfigPayload {
    azureEndpoint: string
    apiVersion: string;
    azureDeployment?: string;
    azureAdToken?: string;
    temperature?: string;
    maxTokens?: number;
    topP?: string;
}

export interface OllamaConfigPayload {
    host?: string;
    options?: Record<string, string>;
}

export interface CreateModelConfigPayload {
  ref: string;
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

export interface MemoryResponse {
  ref: string;
  providerName: string;
  apiKeySecretRef: string;
  apiKeySecretKey: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  memoryParams?: Record<string, any>;
}

export interface PineconeConfigPayload {
    indexHost: string;
    topK?: number;
    namespace?: string;
    recordFields?: string[];
    scoreThreshold?: string;
}

export interface CreateMemoryRequest {
  ref: string;
  provider: Pick<Provider, "type">;
  apiKey: string;
  pinecone?: PineconeConfigPayload;
}

export interface UpdateMemoryRequest {
  ref: string;
  pinecone?: PineconeConfigPayload;
}

/**
 * Feedback issue types
 */
export enum FeedbackIssueType {
  INSTRUCTIONS = "instructions", // Did not follow instructions
  FACTUAL = "factual", // Not factually correct
  INCOMPLETE = "incomplete", // Incomplete response
  TOOL = "tool", // Should have run the tool
  OTHER = "other", // Other
}

/**
* Feedback data structure that will be sent to the API
*/
export interface FeedbackData {
  // Whether the feedback is positive
  isPositive: boolean;

  // The feedback text provided by the user
  feedbackText: string;

  // The type of issue for negative feedback
  issueType?: FeedbackIssueType;

  // ID of the message this feedback pertains to
  messageId: number;
}
