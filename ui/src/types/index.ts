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

export interface FoundryConfig {
  endpoint?: string;
  deployment: string;
  apiVersion?: string;
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
  foundry?: FoundryConfig;
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

export interface UpdateModelConfigPayload {
  apiKey?: string | null;
  spec: ModelConfigSpec;
  secrets?: SecretMaterial[];
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
  /** Populated for sessions owned by another user; use as X-Share-Token to access. */
  share_token?: string | null;
  /** True when the share link that granted access is read-only. */
  share_read_only?: boolean | null;
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
  /** ISO/RFC3339 from Kubernetes `metadata.creationTimestamp` */
  creationTimestamp?: string;
  resourceVersion?: string;
}

export type ToolProviderType = "McpServer" | "Agent"

export interface Tool {
  type: ToolProviderType;
  mcpServer?: McpServerTool;
  agent?: TypedLocalReference;
  /**
   * Agent tools only. When true, each call to the sub-agent gets a fresh
   * A2A context_id (isolated session). Default/false reuses one session.
   * Required for parallel fan-out to the same sub-agent.
   */
  isolateSessions?: boolean;
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

export type AgentType = "Declarative" | "BYO" | "AgentHarness";

/**
 * AgentHarness.spec.backend (go/api/v1alpha3/agentharness_types.go).
 * Single source of truth for backend strings — forms, API payloads, and helpers should use this.
 */
export type AgentHarnessCrBackend =
  | "openclaw"
  | "hermes";
/**
 * Backends that support messenger channels (CR validation + channel form).
 */
export type AgentHarnessMessengerBackend = "openclaw" | "hermes";

export const AGENT_HARNESS_MESSENGER_BACKENDS: readonly AgentHarnessMessengerBackend[] = [
  "openclaw",
  "hermes",
];

/**
 * Backends only available on the Agent Substrate runtime.
 * Mirrors the controller wiring: substrate registers all harness backends.
 */
export const AGENT_HARNESS_SUBSTRATE_ONLY_BACKENDS: readonly AgentHarnessCrBackend[] = [];

/** Single Git repository source for skills. */
export interface GitRepo {
  url: string;
  ref?: string;
  path?: string;
  name?: string;
}

/** Single S3 skill source (prefix or .zip/.tgz archive). */
export interface S3SkillRef {
  uri: string;
  region?: string;
  name?: string;
}

export interface SkillForAgent {
  insecureSkipVerify?: boolean;
  refs?: string[];
  gitAuthSecretRef?: { name: string };
  gitRefs?: GitRepo[];
  s3Refs?: S3SkillRef[];
}

/** Kubernetes SandboxAgent CRD (kagent.dev/v1alpha3). */
export interface SandboxAgent {
  apiVersion?: string;
  kind?: string;
  metadata: ResourceMetadata;
  spec: AgentSpec;
}

export interface SandboxSubstrateSpec {
  workerPoolRef?: { name: string; namespace?: string };
  snapshotsConfig?: { location: string };
}

export interface SandboxConfig {
  network?: { allowedDomains?: string[] };
}

export interface AgentSpec {
  type: AgentType;
  declarative?: DeclarativeAgentSpec;
  byo?: BYOAgentSpec;
  description: string;
  skills?: SkillForAgent;
  substrate?: SandboxSubstrateSpec;
  sandbox?: SandboxConfig;
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
  /** Long-term memory (same shape as Kubernetes declarative spec). */
  memory?: MemorySpec;
  /** When set, systemMessage is rendered as a Go text/template with includes and variables. */
  promptTemplate?: PromptTemplateSpec;
  /** When true, the agent gains built-in share link tools (create/list/delete share tokens). */
  shareTools?: boolean;
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
  image: string;
  cmd?: string;
  args?: string[];
  env?: EnvVar[];
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
      /** RFC3339 from `lastTransitionTime` on Agent conditions */
      lastTransitionTime?: string;
    }>;
  };
}

/** Merged into an AgentHarness list result when Agent Substrate provides the backend. */
export interface AgentHarnessListEntry {
  backend: string;
  actorId?: string;
  /** Same-origin WebSocket path for the ACP chat proxy. */
  acpPath?: string;
  modelConfigRef?: string;
  backendRefId?: string;
  endpoint?: string;
}

/** WorkerPools, ActorTemplates, and ate-api actors/workers returned by GetSubstrateStatus. */
export interface SubstrateStatusResponse {
  enabled: boolean;
  ateApiError?: string;
  workerPools: SubstrateWorkerPoolEntry[];
  actorTemplates: SubstrateActorTemplateEntry[];
  actors: SubstrateActorEntry[];
  workers: SubstrateWorkerEntry[];
}

export interface SubstrateWorkerPoolEntry {
  namespace: string;
  name: string;
  replicas: number;
  ateomImage: string;
}

export interface SubstrateActorTemplateEntry {
  namespace: string;
  name: string;
  phase?: string;
  goldenActorId?: string;
  goldenSnapshot?: string;
  sandboxClass?: string;
  workerSelector?: string;
  harnessName?: string;
  managedByKagent: boolean;
}

export interface SubstrateActorEntry {
  actorId: string;
  status: string;
  actorTemplateNamespace?: string;
  actorTemplateName?: string;
  ateomPodNamespace?: string;
  ateomPodName?: string;
  ateomPodIp?: string;
  latestSnapshot?: string;
  workerPoolName?: string;
  inProgressSnapshot?: string;
  version?: number;
}

export interface SubstrateWorkerEntry {
  workerNamespace: string;
  workerPool: string;
  workerPod: string;
  actorNamespace?: string;
  actorTemplate?: string;
  actorId?: string;
  ip?: string;
  version?: number;
}

export interface AgentResponse {
  id: number | string;
  agent: Agent;
  model: string;
  modelProvider: string;
  modelConfigRef: string;
  tools: Tool[];
  ready: boolean;
  accepted: boolean;
  substrateAgentHarness?: AgentHarnessListEntry;
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

export interface LocalObjectReference {
  name?: string;
}

export interface EnvFromSource {
  prefix?: string;
  configMapRef?: LocalObjectReference & { optional?: boolean };
  secretRef?: LocalObjectReference & { optional?: boolean };
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
  tls?: TLSConfig;
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
  // Optional companion Secrets to create or update alongside the
  // ToolServer. Each entry materializes as a key in an Opaque Secret
  // owned by the created resource so K8s GC cleans up on delete.
  // Names referenced here must match a Secret described in this list
  // (e.g. RemoteMCPServer.spec.tls.caCertSecretRef) for inline
  // materialization; pre-existing Secrets can also be referenced by
  // name without supplying material here.
  secrets?: SecretMaterial[];
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
// These mirror the framework-neutral models in kagent-core/a2a/_hitl.py.
// ---------------------------------------------------------------------------

/** A single tool approval decision value. */
export type ToolDecision = "approve" | "reject";

export const HITL_EXTENSION_URI = "https://kagent.dev/extensions/hitl/v1";

export interface HitlTool {
  id: string;
  call_id: string;
  name: string;
  args: Record<string, unknown>;
}

export interface NestedHitlRequest {
  subagent_name?: string;
  task_id?: string;
  context_id?: string;
  tools: HitlTool[];
}

export interface ToolApprovalRequestPayload {
  type: "tool_approval_request";
  hint?: string;
  tools: HitlTool[];
  nested?: NestedHitlRequest | null;
}

export interface AskUserRequestPayload {
  type: "ask_user_request";
  id: string;
  questions: Array<{ question: string; choices?: string[]; multiple?: boolean }>;
  nested?: NestedHitlRequest | null;
}

export interface ToolApprovalResult {
  id: string;
  approved: boolean;
  rejection_reason?: string;
}

export interface ToolApprovalResponsePayload {
  type: "tool_approval_response";
  approvals: ToolApprovalResult[];
}

export interface AskUserResponsePayload {
  type: "ask_user_response";
  id: string;
  answers?: Array<{ answer: string[] }> | null;
}

export type HitlRequestPayload = ToolApprovalRequestPayload | AskUserRequestPayload;
export type HitlResponsePayload = ToolApprovalResponsePayload | AskUserResponsePayload;
export type HitlExtensionPayload = HitlRequestPayload | HitlResponsePayload;
