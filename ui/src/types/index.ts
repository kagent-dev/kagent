export type ChatStatus = "ready" | "thinking" | "error" | "submitted" | "working" | "input_required" | "auth_required" | "processing_tools" | "generating_response";

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

export type ProviderModel = {
  name: string;
  function_calling: boolean;
}

// Define the type for the expected API response structure
export type ProviderModelsResponse = Record<string, ProviderModel[]>;

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



export interface FunctionCall {
  id: string;
  args: Record<string, unknown>;
  name: string;
}

export interface MCPTool {
  name: string;
  description: string;
  inputSchema: any; // Schema equivalent
}

export interface StdioMcpServerConfig {
  /**
   * The executable to run to start the server.
   */
  command: string;
  /**
   * Command line arguments to pass to the executable.
   */
  args?: string[];
  /**
   * The environment to use when spawning the process.
   */
  env?: Record<string, string>;
}

export interface SseMcpServerConfig {
  url: string;
  headers?: Record<string, any>;
  timeout?: string;
  sseReadTimeout?: string;
}

export interface StreamableHttpMcpServerConfig {
  url: string;
  headers?: Record<string, any>;
  timeout?: string;
  sseReadTimeout?: string;
}

export interface Session {
  id: string;
  name: string;
  agent_id: number;
  user_id: string;
  created_at: string;
  updated_at: string;
  deleted_at: string;
}


export interface ResourceMetadata {
  name: string;
  namespace?: string;
}

export type ToolProviderType = "McpServer" | "Agent"

export interface Tool {
  type: ToolProviderType;
  mcpServer?: McpServerTool;
  agent?: AgentTool;
}

export interface AgentTool {
  ref: string;
  description?: string;
}

export interface McpServerTool {
  toolServer: string;
  toolNames: string[];
}

export interface AgentResourceSpec {
  description: string;
  systemMessage: string;
  tools: Tool[];
  // Name of the model config resource
  modelConfig: string;
  memory?: string[];
}
export interface Agent {
  metadata: ResourceMetadata;
  spec: AgentResourceSpec;
}

export interface AgentResponse {
  id: number;
  agent: Agent;
  model: string;
  modelProvider: string;
  modelConfigRef: string;
  memoryRefs: string[];
  tools: Tool[];
}

export interface ToolServer {
  metadata: ResourceMetadata;
  spec: ToolServerSpec;
}

export interface ToolServerSpec {
  description: string;
  config: ToolServerConfiguration;
}

export interface ToolServerConfiguration {
  stdio?: StdioMcpServerConfig;
  sse?: SseMcpServerConfig;
  streamableHttp?: StreamableHttpMcpServerConfig;
}

export interface ToolServerWithTools {
  ref: string;
  config: ToolServerConfiguration;
  discoveredTools: DiscoveredTool[];
}

export interface DiscoveredTool {
  name: string;
  description: string;
}

export interface ToolResponse {
  id: string;
  server_name: string;
  created_at?: string;
  updated_at?: string;
  deleted_at?: string;
  description?: string;
}