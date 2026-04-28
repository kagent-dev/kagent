export type ChatStatus = "ready" | "thinking" | "error" | "submitted" | "working" | "input_required" | "auth_required" | "processing_tools" | "generating_response";

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

export interface SAPAICoreConfigPayload {
  baseUrl: string;
  resourceGroup?: string;
  authUrl?: string;
}

export interface BedrockConfig {
  region: string;
}

export interface TLSConfig {
  disableVerify?: boolean;
  caCertSecretRef?: string;
  caCertSecretKey?: string;
  disableSystemCAs?: boolean;
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
  sapAICore?: SAPAICoreConfigPayload;
}

export interface ModelConfig {
  ref: string;
  spec: ModelConfigSpec;
}

export interface CreateSessionRequest {
  agent_ref?: string;
  name?: string;
  id?: string;
}

export interface BaseResponse<T> {
  message: string;
  data?: T;
  error?: string;
}

export interface TokenStats {
  total: number;
  prompt: number;
  completion: number;
}

export interface Provider {
  name: string;
  type: string;
  requiredParams: string[];
  optionalParams: string[];
  source?: 'stock' | 'configured'; // Distinguishes between stock and configured providers
  endpoint?: string; // Only present for configured providers
}

export type ProviderModel = {
  name: string;
  function_calling: boolean;
}

// Define the type for the expected API response structure
export type ProviderModelsResponse = Record<string, ProviderModel[]>;

// ConfiguredModelProvider is the response from /api/modelproviderconfigs/configured
export interface ConfiguredModelProvider {
  name: string;
  type: string;
  endpoint: string;
}

// ConfiguredModelProviderModelsResponse is the response from /api/modelproviderconfigs/configured/{name}/models
export interface ConfiguredModelProviderModelsResponse {
  provider: string;
  models: string[];
}

export interface CreateModelConfigRequest {
  ref: string;
  apiKey?: string;
  spec: ModelConfigSpec;
}

export interface UpdateModelConfigPayload {
  apiKey?: string | null;
  spec: ModelConfigSpec;
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
  agent_id: string;
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
  namespace?: string;
}

export interface McpServerTool extends TypedLocalReference {
  toolNames: string[];
  requireApproval?: string[];
}

export type AgentType = "Declarative" | "BYO" | "Sandbox";

/** Single Git repository source for skills. */
export interface GitRepo {
  url: string;
  ref?: string;
  path?: string;
  name?: string;
}

export interface SkillForAgent {
  insecureSkipVerify?: boolean;
  refs?: string[];
  gitAuthSecretRef?: { name: string };
  gitRefs?: GitRepo[];
}

/** Kubernetes SandboxAgent CRD (kagent.dev/v1alpha2). Spec matches Agent.spec (AgentSpec). */
export interface SandboxAgent {
  apiVersion?: string;
  kind?: string;
  metadata: ResourceMetadata;
  spec: AgentSpec;
}

export interface AgentSpec {
  type: AgentType;
  declarative?: DeclarativeAgentSpec;
  byo?: BYOAgentSpec;
  description: string;
  skills?: SkillForAgent;
  memory?: MemorySpec;
}

export interface DeclarativeDeploymentSpec {
  serviceAccountName?: string;
}

/** Prompt library sources referenced for {{include "alias/key"}} in system messages. */
export interface PromptSource {
  kind: string;
  name: string;
  apiGroup?: string;
  alias?: string;
}

export interface PromptTemplateSpec {
  dataSources?: PromptSource[];
}

export interface PromptTemplateSummary {
  namespace: string;
  name: string;
  keyCount: number;
  /** Fragment keys per library (for @ include picker). */
  keys?: string[];
}

export interface PromptTemplateDetail {
  namespace: string;
  name: string;
  data: Record<string, string>;
}

export interface DeclarativeAgentSpec {
  systemMessage: string;
  tools: Tool[];
  // Name of the model config resource
  modelConfig: string;
  stream?: boolean;
  a2aConfig?: A2AConfig;
  context?: ContextConfig;
  deployment?: DeclarativeDeploymentSpec;
  /** Long-term memory (same shape as Kubernetes declarative spec). */
  memory?: MemorySpec;
  /** When set, systemMessage is rendered as a Go text/template with includes and variables. */
  promptTemplate?: PromptTemplateSpec;
}

export interface ContextConfig {
  compaction?: ContextCompressionConfig;
}

export interface ContextCompressionConfig {
  compactionInterval?: number;
  overlapSize?: number;
  summarizer?: ContextSummarizerConfig;
  tokenThreshold?: number;
  eventRetentionSize?: number;
}

export interface ContextSummarizerConfig {
  modelConfig?: string;
  promptTemplate?: string;
}

export interface MemorySpec {
  modelConfig: string;
  ttlDays?: number;
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
  serviceAccountName?: string;
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
  apiVersion?: string;
  kind?: string;
  metadata: ResourceMetadata;
  spec: AgentSpec;
  status?: {
    observedGeneration?: number;
    conditions?: Array<{
      type: string;
      status: string;
      reason?: string;
      message?: string;
    }>;
  };
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
  workloadMode?: "deployment" | "sandbox";
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

export interface AgentMemory {
  id: string;
  content: string;
  access_count: number;
  created_at: string;
  expires_at?: string;
}

// ---------------------------------------------------------------------------
// HITL (Human-in-the-Loop) types
//
// These mirror the Python models in kagent-core/a2a/_hitl_utils.py and describe the
// A2A - UI wire format for request and decision paths in HITL flow.
// ---------------------------------------------------------------------------

/** A single tool approval decision value. */
export type ToolDecision = "approve" | "reject";

/**
 * The resolved approval decision stored on a ToolApprovalRequest message.
 * - A single ToolDecision string for uniform decisions (all approve or all reject).
 * - A per-tool map (keyed by tool call ID) for batch/mixed decisions.
 */
export type ApprovalDecision = ToolDecision | Record<string, ToolDecision>;

// The original tool function call that requires human approval.
export interface HitlOriginalFunctionCall {
  name: string;
  args: Record<string, unknown>;
  id?: string;
}

// Payload stored inside the toolConfirmation field of an adk_request_confirmation DataPart.
export interface HitlToolConfirmationPayload {
  /**
   * For subagent HITL: serialized HitlPartInfo[] from the subagent's own
   * input_required DataParts (see KAgentRemoteA2ATool._handle_input_required).
   * Each entry has the same shape as AdkRequestConfirmationData (without
   * toolConfirmation, since those are leaf-level tool calls).
   */
  hitl_parts?: HitlPartInfo[];
  // The subagent name, set by KAgentRemoteA2ATool.
  subagent_name?: string;
  // Subagent task_id stored for the resume path.
  task_id?: string;
  // Subagent context_id stored for the resume path.
  context_id?: string;
}

// The toolConfirmation field of an adk_request_confirmation DataPart.
export interface HitlToolConfirmation {
  hint?: string;
  confirmed?: boolean;
  payload?: HitlToolConfirmationPayload;
}

// Args of the adk_request_confirmation FunctionCall.
export interface HitlRequestConfirmationArgs {
  originalFunctionCall: HitlOriginalFunctionCall;
  toolConfirmation?: HitlToolConfirmation;
}

// A single serialized HitlPartInfo — the data dict of an adk_request_confirmation DataPart.
export interface HitlPartInfo {
  // Always "adk_request_confirmation".
  name: string;
  // The confirmation function-call ID (distinct from the original FC ID).
  id?: string;
  // The original tool call that requires approval.
  originalFunctionCall: HitlOriginalFunctionCall;
}

// The full data payload of an adk_request_confirmation DataPart, as produced by the ADK event converter and read by the UI.
export interface AdkRequestConfirmationData {
  name: string;
  id: string;
  args: HitlRequestConfirmationArgs;
}
