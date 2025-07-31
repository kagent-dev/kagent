/* eslint-disable @typescript-eslint/no-empty-object-type */
/* eslint-disable @typescript-eslint/no-explicit-any */

// Message Types
export interface RequestUsage {
  prompt_tokens: number;
  completion_tokens: number;
}

export interface ImageContent {
  url: string;
  alt?: string;
  data?: string;
}

export interface FunctionCall {
  id: string;
  arguments: string;
  name: string;
}

export interface FunctionExecutionResult {
  content: string;
}

export interface ToolCallExecutionEvent extends BaseMessageConfig {
  content: Array<{
    content: string;
    name: string;
    call_id: string;
    is_error: boolean;
  }>;
  type: "ToolCallExecutionEvent";
}

export interface BaseMessageConfig {
  source: string;
  models_usage?: RequestUsage;
  metadata?: Record<string, string>;
}

export interface BaseAgentEvent extends BaseMessageConfig {}

export interface MemoryQueryEventContent {
  content: string;
  mime_type: string;
}
export interface MemoryQueryEvent extends BaseAgentEvent {
  content: MemoryQueryEventContent[];
  type: "MemoryQueryEvent";
}

export interface CompletionMessage extends BaseMessageConfig {
  type: "completion";
  status: string;
}

export interface TextMessageConfig extends BaseMessageConfig {
  content: string;
}

export interface ErrorMessageConfig extends BaseMessageConfig {
  data: {
    task_result: TaskResult;
    usage: string;
    duration: number;
  };
  type: "error";
}

export interface MultiModalMessageConfig extends BaseMessageConfig {
  content: (string | ImageContent)[];
}

export interface StopMessageConfig extends BaseMessageConfig {
  content: string;
}

export interface HandoffMessageConfig extends BaseMessageConfig {
  content: string;
  target: string;
}

export interface ToolCallRequestEvent extends BaseMessageConfig {
  content: FunctionCall[];
  type: "ToolCallRequestEvent";
}

export interface ToolCallSummaryMessage extends BaseMessageConfig {
  content: string;
  type: "ToolCallSummaryMessage";
}

export interface ModelClientStreamingChunkEvent extends BaseAgentEvent { 
  content: string;
  type: "ModelClientStreamingChunkEvent";
}

export type AgentMessageConfig = TextMessageConfig | MultiModalMessageConfig | StopMessageConfig | HandoffMessageConfig | ToolCallRequestEvent | ToolCallExecutionEvent | ToolCallSummaryMessage | MemoryQueryEvent | ModelClientStreamingChunkEvent;

// Tool Configs
export interface FunctionToolConfig {
  source_code: string;
  name: string;
  description: string;
  global_imports: any[]; // Sequence[Import] equivalent
  has_cancellation_support: boolean;
}

export interface MCPTool {
  name: string;
  description: string;
  inputSchema: any; // Schema equivalent
}

export interface MCPToolConfig {
  server_params: StdioMcpServerConfig | SseMcpServerConfig;
  tool: MCPTool;
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

// Model Configs
export interface ModelInfo {
  vision?: boolean;
  function_calling: boolean;
  json_output?: boolean;
  family?: string;
}

export interface CreateArgumentsConfig {
  frequency_penalty?: number;
  logit_bias?: Record<string, number>;
  max_tokens?: number;
  n?: number;
  presence_penalty?: number;
  response_format?: any; // ResponseFormat equivalent
  seed?: number;
  stop?: string | string[];
  temperature?: number;
  top_p?: number;
  user?: string;
}

export interface BaseOpenAIClientConfig extends CreateArgumentsConfig {
  model: string;
  api_key?: string;
  timeout?: number;
  max_retries?: number;
  model_capabilities?: any; // ModelCapabilities equivalent
  model_info?: ModelInfo;
}

export interface OpenAIClientConfig extends BaseOpenAIClientConfig {
  organization?: string;
  base_url?: string;
}

export interface AzureOpenAIClientConfig extends BaseOpenAIClientConfig {
  azure_endpoint: string;
  azure_deployment?: string;
  api_version: string;
  azure_ad_token?: string;
  azure_ad_token_provider?: any;
}

export type ModelConfig = OpenAIClientConfig | AzureOpenAIClientConfig;

export type ToolServerConfig = StdioMcpServerConfig | SseMcpServerConfig | StreamableHttpMcpServerConfig;

// DB Models
export interface DBModel {
  id?: number;
  user_id?: string;
  created_at?: string;
  updated_at?: string;
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

export interface TaskResult {
  messages: AgentMessageConfig[];
  stop_reason?: string;
}

export interface TaskResultMessage {
  task_result: TaskResult;
  usage: string;
  duration: number;
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