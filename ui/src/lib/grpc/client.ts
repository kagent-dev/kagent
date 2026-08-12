import "server-only";

import { toJson, type JsonObject } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { Task as A2ATask } from "@a2a-js/sdk";

import {
  AgentHarnessActorState,
  AgentKind,
  AgentService,
  type Agent as AgentMessage,
  type AgentHarnessSessionActor as AgentHarnessSessionActorMessage,
} from "@/generated/kagent/api/v1alpha1/agents_pb";
import {
  ModelService,
  type ModelConfig as ModelConfigMessage,
} from "@/generated/kagent/api/v1alpha1/models_pb";
import {
  PromptTemplateService,
  type PromptTemplate as PromptTemplateMessage,
} from "@/generated/kagent/api/v1alpha1/prompts_pb";
import { FeedbackService } from "@/generated/kagent/api/v1alpha1/feedback_pb";
import {
  EventOrder,
  SessionService,
  TaskStoreService,
  type Session as SessionMessage,
  type SessionEvent as SessionEventMessage,
  type SessionShare as SessionShareMessage,
} from "@/generated/kagent/api/v1alpha1/sessions_pb";
import {
  TaskSchema,
  type Task as TaskMessage,
} from "@/generated/a2a_pb";
import { MemoryService } from "@/generated/kagent/api/v1alpha1/memory_pb";
import {
  SystemService,
  type GetVersionResponse,
} from "@/generated/kagent/api/v1alpha1/system_pb";
import { ToolService } from "@/generated/kagent/api/v1alpha1/tools_pb";
import { getAuthHeadersFromContext } from "@/lib/auth";
import type { CallToolResult, ReadResourceResult } from "@modelcontextprotocol/sdk/types.js";
import type {
  Agent,
  AgentResponse,
  ConfiguredModelProvider,
  ConfiguredModelProviderModelsResponse,
  CreateModelConfigRequest,
  FeedbackData,
  ModelConfig,
  ModelConfigSpec,
  PromptTemplateDetail,
  PromptTemplateSummary,
  Provider,
  ProviderModelsResponse,
  SandboxAgent,
  SubstrateStatusResponse,
  Tool,
  ToolsResponse,
  ToolServerCreateRequest,
  ToolServerResponse,
  RemoteMCPServer,
  MCPServer,
  AgentMemory,
  CreateSessionRequest,
  Session,
  UpdateModelConfigPayload,
} from "@/types";

export const DEFAULT_GRPC_TIMEOUT_MS = 30_000;
const DEFAULT_DEVELOPMENT_GRPC_URL = "http://127.0.0.1:8084";

export type SystemClient = Client<typeof SystemService>;
export type FeedbackClient = Client<typeof FeedbackService>;
export type ModelClient = Client<typeof ModelService>;
export type AgentClient = Client<typeof AgentService>;
export type ToolClient = Client<typeof ToolService>;
export type PromptTemplateClient = Client<typeof PromptTemplateService>;
export type SessionClient = Client<typeof SessionService>;
export type TaskClient = Client<typeof TaskStoreService>;
export type MemoryClient = Client<typeof MemoryService>;

export type AgentKubernetesKind = "SandboxAgent" | "AgentHarness";

export interface AgentHarnessSessionActorResult {
  namespace: string;
  name: string;
  sessionId: string;
  actorId?: string;
  state?: "running" | "suspended" | "missing";
}

export interface NamespaceDto {
  name: string;
  status: string;
}

export interface SessionEventDto {
  id: string;
  session_id: string;
  user_id: string;
  created_at: string;
  updated_at: string;
  deleted_at?: string;
  data: string;
}

export interface SessionWithEventsDto {
  session: Session;
  events: SessionEventDto[];
  read_only?: boolean | null;
}

export interface SessionShareDto {
  token: string;
  session_id: string;
  read_only: boolean;
  created_at: string;
}

type AgentResourceInput = {
  apiVersion?: string;
  kind?: string;
  metadata: Agent["metadata"];
  spec: unknown;
  status?: unknown;
};

export type VersionInfo = Pick<
  GetVersionResponse,
  "kagentVersion" | "gitCommit" | "buildDate"
>;

export class GrpcRequestError extends Error {
  constructor(
    message: string,
    readonly code: Code,
    readonly status: number,
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "GrpcRequestError";
  }
}

let cachedSystemTarget: string | undefined;
let cachedSystemClient: SystemClient | undefined;
let cachedFeedbackTarget: string | undefined;
let cachedFeedbackClient: FeedbackClient | undefined;
let cachedModelTarget: string | undefined;
let cachedModelClient: ModelClient | undefined;
let cachedAgentTarget: string | undefined;
let cachedAgentClient: AgentClient | undefined;
let cachedToolTarget: string | undefined;
let cachedToolClient: ToolClient | undefined;
let cachedPromptTemplateTarget: string | undefined;
let cachedPromptTemplateClient: PromptTemplateClient | undefined;
let cachedSessionTarget: string | undefined;
let cachedSessionClient: SessionClient | undefined;
let cachedTaskTarget: string | undefined;
let cachedTaskClient: TaskClient | undefined;
let cachedMemoryTarget: string | undefined;
let cachedMemoryClient: MemoryClient | undefined;

export function getGrpcTarget(
  env?: { BACKEND_GRPC_URL?: string },
): string {
  const configured = (
    env === undefined ? process.env.BACKEND_GRPC_URL : env.BACKEND_GRPC_URL
  )?.trim();
  const target = configured || DEFAULT_DEVELOPMENT_GRPC_URL;

  let parsed: URL;
  try {
    parsed = new URL(target);
  } catch (error) {
    throw new Error(`BACKEND_GRPC_URL must be an absolute URL: ${target}`, { cause: error });
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("BACKEND_GRPC_URL must use http:// or https://");
  }

  return target.replace(/\/+$/, "");
}

function getSystemClient(): SystemClient {
  const target = getGrpcTarget();
  if (cachedSystemClient === undefined || cachedSystemTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedSystemClient = createClient(SystemService, transport);
    cachedSystemTarget = target;
  }
  return cachedSystemClient;
}

function getFeedbackClient(): FeedbackClient {
  const target = getGrpcTarget();
  if (cachedFeedbackClient === undefined || cachedFeedbackTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedFeedbackClient = createClient(FeedbackService, transport);
    cachedFeedbackTarget = target;
  }
  return cachedFeedbackClient;
}

function getModelClient(): ModelClient {
  const target = getGrpcTarget();
  if (cachedModelClient === undefined || cachedModelTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedModelClient = createClient(ModelService, transport);
    cachedModelTarget = target;
  }
  return cachedModelClient;
}

function getAgentClient(): AgentClient {
  const target = getGrpcTarget();
  if (cachedAgentClient === undefined || cachedAgentTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedAgentClient = createClient(AgentService, transport);
    cachedAgentTarget = target;
  }
  return cachedAgentClient;
}

function getToolClient(): ToolClient {
  const target = getGrpcTarget();
  if (cachedToolClient === undefined || cachedToolTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedToolClient = createClient(ToolService, transport);
    cachedToolTarget = target;
  }
  return cachedToolClient;
}

function getPromptTemplateClient(): PromptTemplateClient {
  const target = getGrpcTarget();
  if (cachedPromptTemplateClient === undefined || cachedPromptTemplateTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedPromptTemplateClient = createClient(PromptTemplateService, transport);
    cachedPromptTemplateTarget = target;
  }
  return cachedPromptTemplateClient;
}

function getSessionClient(): SessionClient {
  const target = getGrpcTarget();
  if (cachedSessionClient === undefined || cachedSessionTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedSessionClient = createClient(SessionService, transport);
    cachedSessionTarget = target;
  }
  return cachedSessionClient;
}

function getTaskClient(): TaskClient {
  const target = getGrpcTarget();
  if (cachedTaskClient === undefined || cachedTaskTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedTaskClient = createClient(TaskStoreService, transport);
    cachedTaskTarget = target;
  }
  return cachedTaskClient;
}

function getMemoryClient(): MemoryClient {
  const target = getGrpcTarget();
  if (cachedMemoryClient === undefined || cachedMemoryTarget !== target) {
    const transport = createGrpcTransport({
      baseUrl: target,
      defaultTimeoutMs: DEFAULT_GRPC_TIMEOUT_MS,
    });
    cachedMemoryClient = createClient(MemoryService, transport);
    cachedMemoryTarget = target;
  }
  return cachedMemoryClient;
}

export async function callGetVersion(
  client: Pick<SystemClient, "getVersion">,
  authHeaders: Record<string, string>,
  timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
): Promise<VersionInfo> {
  try {
    const response = await client.getVersion({}, {
      headers: new Headers(authHeaders),
      timeoutMs,
    });
    return {
      kagentVersion: response.kagentVersion,
      gitCommit: response.gitCommit,
      buildDate: response.buildDate,
    };
  } catch (error) {
    throw mapGrpcError(error);
  }
}

export async function getVersionViaGrpc(): Promise<VersionInfo> {
  const authHeaders = await getAuthHeadersFromContext();
  return callGetVersion(getSystemClient(), authHeaders);
}

export class SystemGrpcGateway {
  constructor(
    private readonly client: SystemClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async getCurrentUserClaims(): Promise<JsonObject> {
    const response = await this.call(() => this.client.getCurrentUser({}, this.options()));
    if (response.claims === undefined) {
      throw new Error("SystemService response did not include current user claims");
    }
    return response.claims;
  }

  async listNamespaces(): Promise<NamespaceDto[]> {
    const response = await this.call(() => this.client.listNamespaces({}, this.options()));
    return response.namespaces.map((namespace) => ({
      name: namespace.name,
      status: namespace.status,
    }));
  }

  async getSubstrateStatus(namespace = ""): Promise<SubstrateStatusResponse> {
    const response = await this.call(() => this.client.getSubstrateStatus({ namespace }, this.options()));
    return {
      enabled: response.enabled,
      ...(response.ateApiError === "" ? {} : { ateApiError: response.ateApiError }),
      workerPools: response.workerPools.map((workerPool) => ({
        namespace: workerPool.namespace,
        name: workerPool.name,
        replicas: workerPool.replicas,
        ateomImage: workerPool.ateomImage,
      })),
      actorTemplates: response.actorTemplates.map((actorTemplate) => ({
        namespace: actorTemplate.namespace,
        name: actorTemplate.name,
        ...(actorTemplate.phase === "" ? {} : { phase: actorTemplate.phase }),
        ...(actorTemplate.goldenActorId === "" ? {} : { goldenActorId: actorTemplate.goldenActorId }),
        ...(actorTemplate.goldenSnapshot === "" ? {} : { goldenSnapshot: actorTemplate.goldenSnapshot }),
        ...(actorTemplate.sandboxClass === "" ? {} : { sandboxClass: actorTemplate.sandboxClass }),
        ...(actorTemplate.workerSelector === "" ? {} : { workerSelector: actorTemplate.workerSelector }),
        ...(actorTemplate.harnessName === "" ? {} : { harnessName: actorTemplate.harnessName }),
        managedByKagent: actorTemplate.managedByKagent,
      })),
      actors: response.actors.map((actor) => ({
        actorId: actor.actorId,
        status: actor.status,
        ...(actor.actorTemplateNamespace === "" ? {} : { actorTemplateNamespace: actor.actorTemplateNamespace }),
        ...(actor.actorTemplateName === "" ? {} : { actorTemplateName: actor.actorTemplateName }),
        ...(actor.ateomPodNamespace === "" ? {} : { ateomPodNamespace: actor.ateomPodNamespace }),
        ...(actor.ateomPodName === "" ? {} : { ateomPodName: actor.ateomPodName }),
        ...(actor.ateomPodIp === "" ? {} : { ateomPodIp: actor.ateomPodIp }),
        ...(actor.latestSnapshot === "" ? {} : { latestSnapshot: actor.latestSnapshot }),
        ...(actor.workerPoolName === "" ? {} : { workerPoolName: actor.workerPoolName }),
        ...(actor.inProgressSnapshot === "" ? {} : { inProgressSnapshot: actor.inProgressSnapshot }),
        ...optionalSafeNumber(actor.version, "Substrate actor version", "version"),
      })),
      workers: response.workers.map((worker) => ({
        workerNamespace: worker.workerNamespace,
        workerPool: worker.workerPool,
        workerPod: worker.workerPod,
        ...(worker.actorNamespace === "" ? {} : { actorNamespace: worker.actorNamespace }),
        ...(worker.actorTemplate === "" ? {} : { actorTemplate: worker.actorTemplate }),
        ...(worker.actorId === "" ? {} : { actorId: worker.actorId }),
        ...(worker.ip === "" ? {} : { ip: worker.ip }),
        ...optionalSafeNumber(worker.version, "Substrate worker version", "version"),
      })),
    };
  }

  private options(): { headers: Headers; timeoutMs: number } {
    return {
      headers: new Headers(this.authHeaders),
      timeoutMs: this.timeoutMs,
    };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getSystemGrpcGateway(): Promise<SystemGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new SystemGrpcGateway(getSystemClient(), authHeaders);
}

export class FeedbackGrpcGateway {
  constructor(
    private readonly client: FeedbackClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async submitFeedback(feedback: FeedbackData): Promise<void> {
    if (!Number.isSafeInteger(feedback.messageId)) {
      throw new Error("Feedback message ID must be a safe integer");
    }
    await this.call(() => this.client.createFeedback({
      messageId: BigInt(feedback.messageId),
      isPositive: feedback.isPositive,
      feedbackText: feedback.feedbackText,
      ...(feedback.issueType === undefined ? {} : { issueType: feedback.issueType }),
    }, this.options()));
  }

  private options(): { headers: Headers; timeoutMs: number } {
    return {
      headers: new Headers(this.authHeaders),
      timeoutMs: this.timeoutMs,
    };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getFeedbackGrpcGateway(): Promise<FeedbackGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new FeedbackGrpcGateway(getFeedbackClient(), authHeaders);
}

export class ModelGrpcGateway {
  constructor(
    private readonly client: ModelClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async listModelConfigs(): Promise<ModelConfig[]> {
    const response = await this.call(() => this.client.listModelConfigs({}, this.options()));
    return response.modelConfigs.map(modelConfigFromMessage);
  }

  async getModelConfig(namespace: string, name: string): Promise<ModelConfig> {
    const response = await this.call(() => this.client.getModelConfig({
      ref: { namespace, name },
    }, this.options()));
    return requiredModelConfig(response.modelConfig);
  }

  async createModelConfig(request: CreateModelConfigRequest): Promise<ModelConfig> {
    const ref = splitResourceRef(request.ref);
    const response = await this.call(() => this.client.createModelConfig({
      ref,
      resource: modelConfigResource(request.spec),
      apiKey: request.apiKey ?? "",
      secrets: request.secrets ?? [],
    }, this.options()));
    return requiredModelConfig(response.modelConfig);
  }

  async updateModelConfig(namespace: string, name: string, request: UpdateModelConfigPayload): Promise<ModelConfig> {
    const response = await this.call(() => this.client.updateModelConfig({
      ref: { namespace, name },
      resource: modelConfigResource(request.spec),
      secrets: request.secrets ?? [],
      ...(typeof request.apiKey === "string" ? { apiKey: request.apiKey } : {}),
    }, this.options()));
    return requiredModelConfig(response.modelConfig);
  }

  async deleteModelConfig(namespace: string, name: string): Promise<void> {
    await this.call(() => this.client.deleteModelConfig({
      ref: { namespace, name },
    }, this.options()));
  }

  async listSupportedModelProviders(): Promise<Provider[]> {
    const response = await this.call(() => this.client.listSupportedModelProviders({}, this.options()));
    return response.providers.map((provider) => ({
      name: provider.name,
      type: provider.type,
      requiredParams: [...provider.requiredParams],
      optionalParams: [...provider.optionalParams],
    }));
  }

  async listSupportedMemoryProviders(): Promise<Provider[]> {
    const response = await this.call(() => this.client.listSupportedMemoryProviders({}, this.options()));
    return response.providers.map((provider) => ({
      name: provider.name,
      type: provider.type,
      requiredParams: [...provider.requiredParams],
      optionalParams: [...provider.optionalParams],
    }));
  }

  async listConfiguredProviders(): Promise<ConfiguredModelProvider[]> {
    const response = await this.call(() => this.client.listConfiguredProviders({}, this.options()));
    return response.providers.map((provider) => ({
      name: provider.name,
      type: provider.type,
      endpoint: provider.endpoint,
    }));
  }

  async listProviderModels(providerName: string, refresh = false): Promise<ConfiguredModelProviderModelsResponse> {
    const response = await this.call(() => this.client.listProviderModels({
      providerName,
      refresh,
    }, this.options()));
    return {
      provider: response.provider,
      models: [...response.models],
    };
  }

  async listSupportedModels(): Promise<ProviderModelsResponse> {
    const response = await this.call(() => this.client.listSupportedModels({}, this.options()));
    const providers: ProviderModelsResponse = {};
    for (const provider of response.providers) {
      providers[provider.provider] = provider.models.map((model) => ({
        name: model.name,
        function_calling: model.functionCalling,
      }));
    }
    return providers;
  }

  private options(): { headers: Headers; timeoutMs: number } {
    return {
      headers: new Headers(this.authHeaders),
      timeoutMs: this.timeoutMs,
    };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getModelGrpcGateway(): Promise<ModelGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new ModelGrpcGateway(getModelClient(), authHeaders);
}

export class AgentGrpcGateway {
  constructor(
    private readonly client: AgentClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async listAgents(namespace = ""): Promise<AgentResponse[]> {
    const response = await this.call(() => this.client.listAgents({ namespace }, this.options()));
    return response.agents.map(agentFromMessage);
  }

  async getAgent(namespace: string, name: string, kind: AgentKubernetesKind = "SandboxAgent"): Promise<AgentResponse> {
    const ref = { namespace, name };
    switch (kind) {
      case "SandboxAgent": {
        const response = await this.call(() => this.client.getSandboxAgent({ ref }, this.options()));
        return agentFromMessage(requiredAgent(response.agent));
      }
      case "AgentHarness": {
        const response = await this.call(() => this.client.getAgentHarness({ ref }, this.options()));
        return agentFromMessage(requiredAgent(response.agent));
      }
      default: {
        const response = await this.call(() => this.client.getSandboxAgent({ ref }, this.options()));
        return agentFromMessage(requiredAgent(response.agent));
      }
    }
  }

  async createSandboxAgent(resource: SandboxAgent): Promise<AgentResponse> {
    const response = await this.call(() => this.client.createSandboxAgent({
      ref: resourceReference(resource),
      resource: structuredAgentResource(resource, "SandboxAgent"),
    }, this.options()));
    return agentFromMessage(requiredAgent(response.agent));
  }

  async updateSandboxAgent(resource: SandboxAgent): Promise<AgentResponse> {
    const response = await this.call(() => this.client.updateSandboxAgent({
      ref: resourceReference(resource),
      resource: structuredAgentResource(resource, "SandboxAgent"),
    }, this.options()));
    return agentFromMessage(requiredAgent(response.agent));
  }

  async createAgentHarness(resource: AgentResourceInput): Promise<AgentResponse> {
    const response = await this.call(() => this.client.createAgentHarness({
      ref: resourceReference(resource),
      resource: structuredAgentResource(resource, "AgentHarness"),
    }, this.options()));
    return agentFromMessage(requiredAgent(response.agent));
  }

  async deleteAgent(namespace: string, name: string, kind: AgentKubernetesKind = "SandboxAgent"): Promise<void> {
    const ref = { namespace, name };
    switch (kind) {
      case "SandboxAgent":
        await this.call(() => this.client.deleteSandboxAgent({ ref }, this.options()));
        return;
      case "AgentHarness":
        await this.call(() => this.client.deleteAgentHarness({ ref }, this.options()));
        return;
      default:
        await this.call(() => this.client.deleteSandboxAgent({ ref }, this.options()));
    }
  }

  async ensureAgentHarnessSessionActor(namespace: string, name: string, sessionId: string): Promise<AgentHarnessSessionActorResult> {
    const response = await this.call(() => this.client.ensureAgentHarnessSessionActor({
      ref: { namespace, name },
      sessionId,
    }, this.options()));
    return actorFromMessage(response.actor);
  }

  async suspendAgentHarnessSessionActor(namespace: string, name: string, sessionId: string): Promise<AgentHarnessSessionActorResult> {
    const response = await this.call(() => this.client.suspendAgentHarnessSessionActor({
      ref: { namespace, name },
      sessionId,
    }, this.options()));
    return actorFromMessage(response.actor);
  }

  async getAgentHarnessSessionActor(namespace: string, name: string, sessionId: string): Promise<AgentHarnessSessionActorResult> {
    const response = await this.call(() => this.client.getAgentHarnessSessionActor({
      ref: { namespace, name },
      sessionId,
    }, this.options()));
    return actorFromMessage(response.actor);
  }

  private options(): { headers: Headers; timeoutMs: number } {
    return {
      headers: new Headers(this.authHeaders),
      timeoutMs: this.timeoutMs,
    };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getAgentGrpcGateway(): Promise<AgentGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new AgentGrpcGateway(getAgentClient(), authHeaders);
}

export interface McpAppToolDto {
  name: string;
  description?: string;
  inputSchema?: unknown;
  uiResourceUri?: string;
  _meta?: Record<string, unknown>;
}

export class ToolGrpcGateway {
  constructor(
    private readonly client: ToolClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async listTools(): Promise<ToolsResponse[]> {
    const response = await this.call(() => this.client.listTools({}, this.options()));
    return response.tools.map((tool) => {
      const value = tool.resource?.value;
      if (!isJsonObject(value)) {
        throw new Error("ToolService response did not include a valid Tool resource");
      }
      return value as unknown as ToolsResponse;
    });
  }

  async listToolServers(): Promise<ToolServerResponse[]> {
    const response = await this.call(() => this.client.listToolServers({}, this.options()));
    return response.toolServers.map((server) => ({
      ref: server.ref,
      groupKind: server.groupKind,
      discoveredTools: server.discoveredTools.map((tool) => ({
        name: tool.name,
        description: tool.description,
      })),
    }));
  }

  async createToolServer(request: ToolServerCreateRequest): Promise<RemoteMCPServer | MCPServer> {
    const resource = request.type === "RemoteMCPServer"
      ? request.remoteMCPServer
      : request.mcpServer;
    if (resource === undefined) {
      throw new Error(`${request.type} resource is required`);
    }
    const ref = toolServerResourceReference(resource);
    const response = await this.call(() => this.client.createToolServer({
      type: request.type,
      ref,
      resource: {
        apiVersion: request.type === "RemoteMCPServer" ? "kagent.dev/v1alpha2" : "kagent.dev/v1alpha1",
        kind: request.type,
        value: toJsonObject(resource, `${request.type} resource`),
      },
      secrets: request.secrets ?? [],
    }, this.options()));
    const value = response.resource?.value;
    if (!isJsonObject(value)) {
      throw new Error(`ToolService response did not include a valid ${request.type} resource`);
    }
    return value as unknown as RemoteMCPServer | MCPServer;
  }

  async deleteToolServer(namespace: string, name: string): Promise<void> {
    await this.call(() => this.client.deleteToolServer({
      ref: { namespace, name },
    }, this.options()));
  }

  async listToolServerTypes(): Promise<string[]> {
    const response = await this.call(() => this.client.listToolServerTypes({}, this.options()));
    return [...response.types];
  }

  async listMcpAppTools(namespace: string, name: string, groupKind = ""): Promise<McpAppToolDto[]> {
    const response = await this.call(() => this.client.listMCPAppTools({
      server: mcpServerReference(namespace, name, groupKind),
    }, this.options()));
    return response.tools.map((tool) => ({
      name: tool.name,
      ...(tool.description === "" ? {} : { description: tool.description }),
      ...(tool.inputSchema?.value === undefined ? {} : { inputSchema: tool.inputSchema.value }),
      ...(tool.uiResourceUri === "" ? {} : { uiResourceUri: tool.uiResourceUri }),
      ...(tool.meta?.value === undefined ? {} : { _meta: tool.meta.value as Record<string, unknown> }),
    }));
  }

  async callMcpAppTool(
    namespace: string,
    name: string,
    toolName: string,
    args: Record<string, unknown> = {},
    groupKind = "",
  ): Promise<CallToolResult> {
    const response = await this.call(() => this.client.callMCPAppTool({
      server: mcpServerReference(namespace, name, groupKind),
      toolName,
      arguments: {
        apiVersion: "mcp.kagent.dev/v1alpha1",
        kind: "MCPArguments",
        value: toJsonObject(args, "MCP tool arguments"),
      },
    }, this.options()));
    return requiredStructuredResult<CallToolResult>(response.result?.value, "MCP tool result");
  }

  async readMcpAppResource(
    namespace: string,
    name: string,
    uri: string,
    groupKind = "",
  ): Promise<ReadResourceResult> {
    const response = await this.call(() => this.client.readMCPAppResource({
      server: mcpServerReference(namespace, name, groupKind),
      uri,
    }, this.options()));
    return requiredStructuredResult<ReadResourceResult>(response.result?.value, "MCP resource result");
  }

  private options(): { headers: Headers; timeoutMs: number } {
    return {
      headers: new Headers(this.authHeaders),
      timeoutMs: this.timeoutMs,
    };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getToolGrpcGateway(): Promise<ToolGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new ToolGrpcGateway(getToolClient(), authHeaders);
}

export class PromptTemplateGrpcGateway {
  constructor(
    private readonly client: PromptTemplateClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async listPromptTemplates(namespace: string): Promise<PromptTemplateSummary[]> {
    const response = await this.call(() => this.client.listPromptTemplates({ namespace }, this.options()));
    return response.promptTemplates.map((summary) => {
      const ref = requiredPromptTemplateRef(summary.ref, "summary");
      return {
        namespace: ref.namespace,
        name: ref.name,
        keyCount: summary.keyCount,
        keys: [...summary.keys],
      };
    });
  }

  async getPromptTemplate(namespace: string, name: string): Promise<PromptTemplateDetail> {
    const response = await this.call(() => this.client.getPromptTemplate({
      ref: { namespace, name },
    }, this.options()));
    return promptTemplateFromMessage(response.promptTemplate);
  }

  async createPromptTemplate(
    namespace: string,
    name: string,
    data: Record<string, string>,
  ): Promise<PromptTemplateDetail> {
    const response = await this.call(() => this.client.createPromptTemplate({
      ref: { namespace, name },
      data,
    }, this.options()));
    return promptTemplateFromMessage(response.promptTemplate);
  }

  async updatePromptTemplate(
    namespace: string,
    name: string,
    data: Record<string, string>,
  ): Promise<PromptTemplateDetail> {
    const response = await this.call(() => this.client.updatePromptTemplate({
      ref: { namespace, name },
      data,
    }, this.options()));
    return promptTemplateFromMessage(response.promptTemplate);
  }

  async deletePromptTemplate(namespace: string, name: string): Promise<void> {
    await this.call(() => this.client.deletePromptTemplate({
      ref: { namespace, name },
    }, this.options()));
  }

  private options(): { headers: Headers; timeoutMs: number } {
    return {
      headers: new Headers(this.authHeaders),
      timeoutMs: this.timeoutMs,
    };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getPromptTemplateGrpcGateway(): Promise<PromptTemplateGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new PromptTemplateGrpcGateway(getPromptTemplateClient(), authHeaders);
}

export class SessionGrpcGateway {
  constructor(
    private readonly sessionClient: SessionClient,
    private readonly taskClient: TaskClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async deleteSession(sessionId: string): Promise<void> {
    await this.call(() => this.sessionClient.deleteSession({ sessionId }, this.options()));
  }

  async getSession(sessionId: string, shareToken?: string): Promise<Session> {
    const response = await this.getSessionResponse(sessionId, shareToken);
    return requiredSession(response.session);
  }

  async getSessionWithEvents(sessionId: string, shareToken?: string): Promise<SessionWithEventsDto> {
    const response = await this.getSessionResponse(sessionId, shareToken);
    return {
      session: requiredSession(response.session),
      events: response.events.map(sessionEventFromMessage),
      ...(response.readOnly === undefined ? {} : { read_only: response.readOnly }),
    };
  }

  async listSessionsByAgent(namespace: string, name: string): Promise<Session[]> {
    const response = await this.call(() => this.sessionClient.listSessionsByAgent({
      agentRef: { namespace, name },
    }, this.options()));
    return response.sessions.map(sessionFromMessage);
  }

  async createSession(request: CreateSessionRequest): Promise<Session> {
    const response = await this.call(() => this.sessionClient.createSession({
      agentRef: request.agent_ref ?? "",
      ...(request.id === undefined ? {} : { id: request.id }),
      ...(request.name === undefined ? {} : { name: request.name }),
    }, this.options()));
    return requiredSession(response.session);
  }

  async renameSession(sessionId: string, name: string): Promise<Session> {
    const response = await this.call(() => this.sessionClient.updateSession({
      sessionId,
      name,
    }, this.options()));
    return requiredSession(response.session);
  }

  async listTasks(sessionId: string, shareToken?: string): Promise<A2ATask[]> {
    const response = await this.call(() => this.taskClient.listTasks(
      { sessionId },
      this.options(shareToken),
    ));
    return response.tasks.map(requiredTask);
  }

  async createSessionShare(sessionId: string, readOnly = true): Promise<SessionShareDto> {
    const response = await this.call(() => this.sessionClient.createSessionShare({
      sessionId,
      readOnly,
    }, this.options()));
    return sessionShareFromMessage(response.share);
  }

  async listSessionShares(sessionId: string): Promise<SessionShareDto[]> {
    const response = await this.call(() => this.sessionClient.listSessionShares({ sessionId }, this.options()));
    return response.shares.map((share) => sessionShareFromMessage(share));
  }

  async deleteSessionShare(sessionId: string, token: string): Promise<void> {
    await this.call(() => this.sessionClient.deleteSessionShare({ sessionId, token }, this.options()));
  }

  private async getSessionResponse(sessionId: string, shareToken?: string) {
    return this.call(() => this.sessionClient.getSession({
      sessionId,
      order: EventOrder.DESCENDING,
    }, this.options(shareToken)));
  }

  private options(shareToken?: string): { headers: Headers; timeoutMs: number } {
    const headers = new Headers(this.authHeaders);
    if (shareToken !== undefined) {
      headers.set("x-share-token", shareToken);
    }
    return { headers, timeoutMs: this.timeoutMs };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getSessionGrpcGateway(): Promise<SessionGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new SessionGrpcGateway(getSessionClient(), getTaskClient(), authHeaders);
}

export class MemoryGrpcGateway {
  constructor(
    private readonly client: MemoryClient,
    private readonly authHeaders: Record<string, string>,
    private readonly timeoutMs = DEFAULT_GRPC_TIMEOUT_MS,
  ) {}

  async listAgentMemories(agentName: string, userId: string): Promise<AgentMemory[]> {
    const response = await this.call(() => this.client.list({
      agentName,
      userId,
    }, this.options()));
    return response.memories.map((memory) => ({
      id: memory.id,
      content: memory.content,
      access_count: requiredSafeNumber(memory.accessCount, "Memory access count"),
      created_at: timestampISO(memory.createdAt),
      ...(memory.expiresAt === undefined ? {} : { expires_at: timestampISO(memory.expiresAt) }),
    }));
  }

  async clearAgentMemory(agentName: string, userId: string): Promise<void> {
    await this.call(() => this.client.delete({ agentName, userId }, this.options()));
  }

  private options(): { headers: Headers; timeoutMs: number } {
    return {
      headers: new Headers(this.authHeaders),
      timeoutMs: this.timeoutMs,
    };
  }

  private async call<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation();
    } catch (error) {
      throw mapGrpcError(error);
    }
  }
}

export async function getMemoryGrpcGateway(): Promise<MemoryGrpcGateway> {
  const authHeaders = await getAuthHeadersFromContext();
  return new MemoryGrpcGateway(getMemoryClient(), authHeaders);
}

function requiredSession(message: SessionMessage | undefined): Session {
  if (message === undefined) {
    throw new Error("SessionService response did not include a Session");
  }
  return sessionFromMessage(message);
}

function sessionFromMessage(message: SessionMessage): Session {
  return {
    id: message.id,
    name: message.name ?? "",
    agent_id: message.agentId ?? "",
    user_id: message.userId,
    created_at: timestampISO(message.createdAt),
    updated_at: timestampISO(message.updatedAt),
    deleted_at: message.deletedAt === undefined ? "" : timestampISO(message.deletedAt),
    ...(message.shareToken === undefined ? {} : { share_token: message.shareToken }),
    ...(message.shareReadOnly === undefined ? {} : { share_read_only: message.shareReadOnly }),
  };
}

function sessionEventFromMessage(message: SessionEventMessage): SessionEventDto {
  return {
    id: message.id,
    session_id: message.sessionId,
    user_id: message.userId,
    created_at: timestampISO(message.createdAt),
    updated_at: timestampISO(message.updatedAt),
    ...(message.deletedAt === undefined ? {} : { deleted_at: timestampISO(message.deletedAt) }),
    data: message.data,
  };
}

function sessionShareFromMessage(message: SessionShareMessage | undefined): SessionShareDto {
  if (message === undefined) {
    throw new Error("SessionService response did not include a SessionShare");
  }
  requiredSafeNumber(message.id, "Session share ID");
  return {
    token: message.token,
    session_id: message.sessionId,
    read_only: message.readOnly,
    created_at: timestampISO(message.createdAt),
  };
}

function requiredTask(value: TaskMessage): A2ATask {
  return A2ATask.fromJSON(toJson(TaskSchema, value));
}

function timestampISO(timestamp: Parameters<typeof timestampDate>[0] | undefined): string {
  if (timestamp === undefined) {
    return "";
  }
  return timestampDate(timestamp).toISOString();
}

function requiredSafeNumber(value: bigint, description: string): number {
  const converted = Number(value);
  if (!Number.isSafeInteger(converted)) {
    throw new Error(`${description} exceeds the JavaScript safe integer range`);
  }
  return converted;
}

function toolServerResourceReference(resource: RemoteMCPServer | MCPServer): { namespace: string; name: string } {
  const namespace = resource.metadata.namespace?.trim() ?? "";
  const name = resource.metadata.name.trim();
  if (name === "") {
    throw new Error("ToolServer resource name is required");
  }
  return { namespace, name };
}

function mcpServerReference(namespace: string, name: string, groupKind: string) {
  return {
    ref: { namespace, name },
    groupKind,
  };
}

function requiredStructuredResult<T>(value: JsonObject | undefined, description: string): T {
  if (!isJsonObject(value)) {
    throw new Error(`ToolService response did not include a valid ${description}`);
  }
  return value as unknown as T;
}

function requiredPromptTemplateRef(
  ref: { namespace: string; name: string } | undefined,
  description: string,
): { namespace: string; name: string } {
  if (ref === undefined || ref.namespace === "" || ref.name === "") {
    throw new Error(`PromptTemplateService response did not include a complete ${description} reference`);
  }
  return ref;
}

function promptTemplateFromMessage(message: PromptTemplateMessage | undefined): PromptTemplateDetail {
  if (message === undefined) {
    throw new Error("PromptTemplateService response did not include a PromptTemplate");
  }
  const ref = requiredPromptTemplateRef(message.ref, "PromptTemplate");
  return {
    namespace: ref.namespace,
    name: ref.name,
    data: { ...message.data },
  };
}

function requiredModelConfig(modelConfig: ModelConfigMessage | undefined): ModelConfig {
  if (modelConfig === undefined) {
    throw new Error("ModelService response did not include a ModelConfig");
  }
  return modelConfigFromMessage(modelConfig);
}

function modelConfigFromMessage(modelConfig: ModelConfigMessage): ModelConfig {
  const ref = modelConfig.ref;
  if (ref === undefined || ref.namespace === "" || ref.name === "") {
    throw new Error("ModelService response did not include a complete ModelConfig reference");
  }
  const value = modelConfig.resource?.value;
  if (!isJsonObject(value) || !isJsonObject(value.spec)) {
    throw new Error("ModelService response did not include a valid ModelConfig spec");
  }
  return {
    ref: `${ref.namespace}/${ref.name}`,
    spec: value.spec as unknown as ModelConfigSpec,
  };
}

function modelConfigResource(spec: ModelConfigSpec) {
  return {
    apiVersion: "kagent.dev/v1alpha2",
    kind: "ModelConfig",
    value: toJsonObject({ spec }),
  };
}

function splitResourceRef(ref: string): { namespace: string; name: string } {
  const separator = ref.indexOf("/");
  if (separator <= 0 || separator === ref.length - 1 || ref.indexOf("/", separator + 1) !== -1) {
    throw new Error("ModelConfig reference must use namespace/name format");
  }
  return {
    namespace: ref.slice(0, separator),
    name: ref.slice(separator + 1),
  };
}

function toJsonObject(value: unknown, resourceName = "resource"): JsonObject {
  const serialized = JSON.stringify(value);
  if (serialized === undefined) {
    throw new Error(`${resourceName} is not JSON serializable`);
  }
  const parsed: unknown = JSON.parse(serialized);
  if (!isJsonObject(parsed)) {
    throw new Error(`${resourceName} must be a JSON object`);
  }
  return parsed;
}

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function optionalSafeNumber<Key extends string>(
  value: bigint,
  description: string,
  key: Key,
): Partial<Record<Key, number>> {
  if (value === BigInt(0)) {
    return {};
  }
  const converted = Number(value);
  if (!Number.isSafeInteger(converted)) {
    throw new Error(`${description} exceeds the JavaScript safe integer range`);
  }
  return { [key]: converted } as Partial<Record<Key, number>>;
}

function resourceReference(resource: AgentResourceInput): { namespace: string; name: string } {
  const namespace = resource.metadata.namespace?.trim() ?? "";
  const name = resource.metadata.name.trim();
  if (name === "") {
    throw new Error("Agent resource name is required");
  }
  return { namespace, name };
}

function structuredAgentResource(resource: AgentResourceInput, kind: AgentKubernetesKind) {
  return {
    apiVersion: resource.apiVersion || "kagent.dev/v1alpha3",
    kind,
    value: toJsonObject(resource, `${kind} resource`),
  };
}

function requiredAgent(agent: AgentMessage | undefined): AgentMessage {
  if (agent === undefined) {
    throw new Error("AgentService response did not include an Agent");
  }
  return agent;
}

function agentFromMessage(message: AgentMessage): AgentResponse {
  const ref = message.ref;
  if (ref === undefined || ref.namespace === "" || ref.name === "") {
    throw new Error("AgentService response did not include a complete Agent reference");
  }
  const kind = agentKindName(message.kind);
  const value = message.resource?.value;
  if (!isJsonObject(value)) {
    throw new Error(`AgentService response did not include a valid ${kind} resource`);
  }
  const rawMetadata = isJsonObject(value.metadata) ? value.metadata : {};
  const metadata = {
    ...rawMetadata,
    name: ref.name,
    namespace: ref.namespace,
  } as Agent["metadata"];
  const rawSpec = isJsonObject(value.spec) ? value.spec : {};
  const agent: Agent = kind === "AgentHarness"
    ? {
        apiVersion: message.resource?.apiVersion || "kagent.dev/v1alpha3",
        kind,
        metadata,
        spec: {
          description: typeof rawSpec.description === "string" ? rawSpec.description.trim() : "",
        } as Agent["spec"],
      }
    : {
        ...(value as unknown as Agent),
        apiVersion: message.resource?.apiVersion || "kagent.dev/v1alpha3",
        kind,
        metadata,
        spec: rawSpec as unknown as Agent["spec"],
      };

  const modelConfigRef = message.modelConfigRef === undefined || message.modelConfigRef.name === ""
    ? ""
    : message.modelConfigRef.namespace === ""
      ? message.modelConfigRef.name
      : `${message.modelConfigRef.namespace}/${message.modelConfigRef.name}`;
  const tools = message.tools.map((tool) => {
    if (!isJsonObject(tool.value)) {
      throw new Error("AgentService response included an invalid Agent tool");
    }
    return tool.value as unknown as Tool;
  });
  return {
    id: message.id,
    agent,
    model: message.model,
    modelProvider: message.modelProvider,
    modelConfigRef,
    tools,
    ready: message.ready,
    accepted: message.accepted,
    ...(message.agentHarness === undefined ? {} : {
      substrateAgentHarness: {
        backend: message.agentHarness.backend,
        actorId: message.agentHarness.actorId || undefined,
        acpPath: message.agentHarness.acpPath || undefined,
        modelConfigRef: modelConfigRef || undefined,
        backendRefId: message.agentHarness.backendRefId || undefined,
        endpoint: message.agentHarness.endpoint || undefined,
      },
    }),
  };
}

function agentKindName(kind: AgentKind): AgentKubernetesKind {
  switch (kind) {
    case AgentKind.SANDBOX_AGENT:
      return "SandboxAgent";
    case AgentKind.AGENT_HARNESS:
      return "AgentHarness";
    default:
      throw new Error(`AgentService response included an unknown Agent kind ${kind}`);
  }
}

function actorFromMessage(actor: AgentHarnessSessionActorMessage | undefined): AgentHarnessSessionActorResult {
  if (actor?.ref === undefined || actor.ref.namespace === "" || actor.ref.name === "" || actor.sessionId === "") {
    throw new Error("AgentService response did not include a complete session actor");
  }
  const state = actor.state === AgentHarnessActorState.RUNNING
    ? "running"
    : actor.state === AgentHarnessActorState.SUSPENDED
      ? "suspended"
      : actor.state === AgentHarnessActorState.MISSING
        ? "missing"
        : undefined;
  return {
    namespace: actor.ref.namespace,
    name: actor.ref.name,
    sessionId: actor.sessionId,
    ...(actor.actorId === "" ? {} : { actorId: actor.actorId }),
    ...(state === undefined ? {} : { state }),
  };
}

export function mapGrpcError(error: unknown): GrpcRequestError {
  const connectError = ConnectError.from(error);
  return new GrpcRequestError(
    grpcErrorMessage(connectError),
    connectError.code,
    grpcCodeToHttpStatus(connectError.code),
    { cause: error },
  );
}

function grpcErrorMessage(error: ConnectError): string {
  switch (error.code) {
    case Code.DeadlineExceeded:
      return "Request timed out - server took too long to respond.";
    case Code.Unavailable:
      return "Network error - Could not reach backend server.";
    default:
      return error.rawMessage || "gRPC request failed";
  }
}

function grpcCodeToHttpStatus(code: Code): number {
  switch (code) {
    case Code.InvalidArgument:
      return 400;
    case Code.Unauthenticated:
      return 401;
    case Code.PermissionDenied:
      return 403;
    case Code.NotFound:
      return 404;
    case Code.AlreadyExists:
    case Code.Aborted:
      return 409;
    case Code.FailedPrecondition:
      return 412;
    case Code.ResourceExhausted:
      return 429;
    case Code.Canceled:
      return 499;
    case Code.DeadlineExceeded:
      return 504;
    case Code.Unimplemented:
      return 501;
    case Code.Unavailable:
      return 503;
    default:
      return 500;
  }
}
