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
  reasoningEffort?: string;
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

export interface GeminiConfigPayload {
  baseUrl?: string;
  temperature?: string;
  maxTokens?: number;
  topP?: string;
  topK?: number;
}

export interface GeminiVertexAIConfigPayload {
  project?: string;
  location?: string;
  temperature?: string;
  maxTokens?: number;
  topP?: string;
  topK?: number;
}

export interface AnthropicVertexAIConfigPayload {
  project?: string;
  location?: string;
  temperature?: string;
  maxTokens?: number;
  topP?: string;
  topK?: number;
}

export interface CreateModelConfigRequest {
  ref: string;
  provider: Pick<Provider, "name" | "type">;
  model: string;
  apiKey: string;
  openAI?: OpenAIConfigPayload;
  anthropic?: AnthropicConfigPayload;
  azureOpenAI?: AzureOpenAIConfigPayload;
  ollama?: OllamaConfigPayload;
  gemini?: GeminiConfigPayload;
  geminiVertexAI?: GeminiVertexAIConfigPayload;
  anthropicVertexAI?: AnthropicVertexAIConfigPayload;
}

export interface UpdateModelConfigPayload {
  provider: Pick<Provider, "name" | "type">;
  model: string;
  apiKey?: string | null;
  openAI?: OpenAIConfigPayload;
  anthropic?: AnthropicConfigPayload;
  azureOpenAI?: AzureOpenAIConfigPayload;
  ollama?: OllamaConfigPayload;
  gemini?: GeminiConfigPayload;
  geminiVertexAI?: GeminiVertexAIConfigPayload;
  anthropicVertexAI?: AnthropicVertexAIConfigPayload;
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

export interface Session {
  id: string;
  name: string;
  agent_id: number;
  user_id: string;
  created_at: string;
  updated_at: string;
  deleted_at: string;
}

export interface ToolsResponse {
  id: string;
  server_name: string;
  created_at: string;
  updated_at: string;
  deleted_at: string;
  description: string;
  group_kind: string;
}


export interface ResourceMetadata {
  name: string;
  namespace?: string;
}

export type ToolProviderType = "McpServer" | "Agent"

export interface Tool {
  type: ToolProviderType;
  mcpServer?: McpServerTool;
  agent?: TypedLocalReference;
}

export interface TypedLocalReference {
  kind?: string;
  apiGroup?: string;
  name: string;
}

export interface McpServerTool extends TypedLocalReference {
  toolNames: string[];
}

export type AgentType = "Declarative" | "BYO";
export interface AgentSpec {
  type: AgentType;
  declarative?: DeclarativeAgentSpec;
  byo?: BYOAgentSpec;
  description: string;
}

export interface DeclarativeAgentSpec {
  systemMessage: string;
  tools: Tool[];
  // Name of the model config resource
  modelConfig: string;
  stream?: boolean;
  a2aConfig?: A2AConfig;
}

export interface BYOAgentSpec {
  deployment: BYODeploymentSpec;
}

export interface BYODeploymentSpec {
  image: string;
  cmd?: string;
  args?: string[];

  // Items from the SharedDeploymentSpec
  replicas?: number;
  imagePullSecrets?: Array<{ name: string }>;
  volumes?: unknown[];
  volumeMounts?: unknown[];
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  env?: EnvVar[];
  imagePullPolicy?: string;
}

export interface A2AConfig {
  skills: AgentSkill[];
}

export interface AgentSkill {
  id: string
  name: string;
  description?: string;
  tags: string[];
  examples: string[];
  inputModes: string[];
  outputModes: string[];
}


export interface Agent {
  metadata: ResourceMetadata;
  spec: AgentSpec;
}

export interface AgentResponse {
  id: number;
  agent: Agent;
  model: string;
  modelProvider: string;
  modelConfigRef: string;
  tools: Tool[];
  deploymentReady: boolean;
  accepted: boolean;
}

export interface RemoteMCPServer {
  metadata: ResourceMetadata;
  spec: RemoteMCPServerSpec;
}

export interface SecretKeySelector {
  name: string;
  key: string;
  optional?: boolean;
}

export interface EnvVarSource {
  secretKeyRef?: SecretKeySelector;
}

export interface EnvVar {
  name: string;
  value?: string;
  valueFrom?: EnvVarSource;
}

export interface ValueSource {
  type: string;
  name: string;
  key: string;
}

export interface ValueRef {
  name: string;
  value?: string;
  valueFrom?: ValueSource;
}

export type RemoteMCPServerProtocol = "SSE" | "STREAMABLE_HTTP"

export interface RemoteMCPServerSpec {
  description: string;
  protocol: RemoteMCPServerProtocol;
  url: string;
  headersFrom: ValueRef[];
  timeout?: string;
  sseReadTimeout?: string;
  terminateOnClose?: boolean;
}

export interface RemoteMCPServerResponse {
  ref: string; // namespace/name
  groupKind: string;
  discoveredTools: DiscoveredTool[];
}

// MCPServer types for stdio-based servers
export interface MCPServerDeployment {
  image: string;
  port: number;
  cmd?: string;
  args?: string[];
  env?: Record<string, string>;
}

// eslint-disable-next-line @typescript-eslint/no-empty-object-type
export interface StdioTransport {
  // Empty interface for stdio transport
}

export type TransportType = "stdio";

export interface MCPServerSpec {
  deployment: MCPServerDeployment;
  transportType: TransportType;
  stdioTransport: StdioTransport;
}

export interface MCPServer {
  metadata: {
    name: string;
    namespace: string;
  };
  spec: MCPServerSpec;
}

export interface MCPServerResponse {
  ref: string; // namespace/name
  groupKind: string;
  discoveredTools: DiscoveredTool[];
}

// Union type for tool server responses
export type ToolServerResponse = RemoteMCPServerResponse | MCPServerResponse;

// Union type for tool server creation
export type ToolServer = RemoteMCPServer | MCPServer;

// Tool server creation request
export interface ToolServerCreateRequest {
  type: "RemoteMCPServer" | "MCPServer";
  remoteMCPServer?: RemoteMCPServer;
  mcpServer?: MCPServer;
}


export interface DiscoveredTool {
  name: string;
  description: string;
}
