/** Agent domain models, mirroring the `kagent.dev/v1alpha2` Agent CRD. */

import type { ResourceMetadata, TypedLocalReference } from "./common";

export type AgentType = "Declarative" | "BYO" | "AgentHarness";

/** Which ADK implementation runs a declarative agent. */
export type DeclarativeRuntime = "python" | "go";

export type ToolProviderType = "McpServer" | "Agent";

export interface McpServerTool extends TypedLocalReference {
  /**
   * The tools taken from this server, or absent for all of them.
   *
   * Optional in the CRD — "the names of the tools to be provided by the ToolServer" with
   * no minimum — and it was required here, which is a shape the controller can return
   * and this client could not describe.
   */
  toolNames?: string[];
  requireApproval?: string[];
}

/** One tool binding on an agent: either an MCP server or another agent. */
export interface Tool {
  type: ToolProviderType;
  mcpServer?: McpServerTool;
  agent?: TypedLocalReference;
}

export interface AgentSkill {
  id: string;
  name: string;
  description?: string;
  tags: string[];
  examples: string[];
  inputModes: string[];
  outputModes: string[];
}

export interface A2AConfig {
  skills: AgentSkill[];
}

/** One git repository a skill package is fetched from. */
export interface GitSkillRepo {
  url: string;
  /** Branch, tag or commit SHA. The controller defaults this to "main". */
  ref?: string;
  /** Subdirectory within the repo used as the skill root. */
  path?: string;
  /** Directory name under /skills. Defaults to the last segment of path, or the repo. */
  name?: string;
}

/**
 * Skill packages fetched into the agent's filesystem under `/skills`.
 *
 * Not the same thing as `a2aConfig.skills`, and the two are easy to confuse: those are
 * capabilities an agent *advertises* to callers, while these are files it is *given*.
 * An agent can have either, both or neither, so anything showing "skills" has to say
 * which kind it means.
 *
 * `refs` are OCI images, `gitRefs` are repositories. The CRD has no S3 field in
 * v1alpha2 — the UI this replaced rendered one, which is why it is worth stating.
 */
export interface SkillPackages {
  refs?: string[];
  gitRefs?: GitSkillRepo[];
  /** Development only: pull over HTTP and skip certificate verification. */
  insecureSkipVerify?: boolean;
}

export interface ContextCompressionConfig {
  compactionInterval?: number;
  overlapSize?: number;
  tokenThreshold?: number;
  eventRetentionSize?: number;
  summarizer?: {
    modelConfig?: string;
    promptTemplate?: string;
  };
}

export interface ContextConfig {
  compaction?: ContextCompressionConfig;
}

export interface MemorySpec {
  modelConfig: string;
  ttlDays?: number;
}

/** Prompt libraries an agent's system message may `include`. */
export interface PromptTemplateSpec {
  dataSources?: Array<{
    kind: string;
    name: string;
    apiGroup?: string;
    alias?: string;
  }>;
}

export interface DeclarativeAgentSpec {
  /*
   * No `runtime`, and no `deployment`.
   *
   * Both were `v1alpha2` fields and neither survived into `v1alpha3`, whose CRD
   * decodes strictly: sending either is not ignored but fatal to the whole resource
   * — `unknown field "spec.declarative.runtime"` — so every agent create failed with
   * "Invalid Agent resource" and nothing said which field the controller meant.
   *
   * What v1alpha3 added in their place is `env` and `imageRegistry`, which this form
   * does not offer yet.
   */
  systemMessage: string;
  tools: Tool[];
  /** Ref of the ModelConfig this agent runs on. */
  modelConfig: string;
  stream?: boolean;
  /** Grant the agent built-in share-link creation/deletion tools. */
  shareTools?: boolean;
  a2aConfig?: A2AConfig;
  /** Skill packages mounted under /skills. See `SkillPackages`. */
  skills?: SkillPackages;
  context?: ContextConfig;
  memory?: MemorySpec;
  promptTemplate?: PromptTemplateSpec;
}

export interface BYOEnvVar {
  name: string;
  value?: string;
  valueFrom?: {
    secretKeyRef?: { name: string; key: string; optional?: boolean };
  };
}

export interface BYODeploymentSpec {
  image: string;
  cmd?: string;
  args?: string[];
  replicas?: number;
  imagePullPolicy?: string;
  serviceAccountName?: string;
  imagePullSecrets?: Array<{ name: string }>;
  env?: BYOEnvVar[];
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface BYOAgentSpec {
  deployment: BYODeploymentSpec;
}

/**
 * Substrate actor configuration for sandbox agents.
 *
 * When present the controller registers the agent as an ate.dev actor — one per
 * chat session — rather than as a long-lived Deployment.
 */
export interface SubstrateAgentConfig {
  /** Worker pool the actor is scheduled on. Omit to use the namespace default. */
  workerPoolRef?: { name: string };
  /** Where snapshots are persisted between sessions. */
  snapshotsConfig?: { location: string };
}

export interface AgentSpec {
  type: AgentType;
  description: string;
  declarative?: DeclarativeAgentSpec;
  byo?: BYOAgentSpec;
  /** Present when the agent is a substrate sandbox actor. */
  substrate?: SubstrateAgentConfig;
}

export interface AgentStatusCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface Agent {
  apiVersion?: string;
  kind?: string;
  metadata: ResourceMetadata;
  spec: AgentSpec;
  status?: {
    observedGeneration?: number;
    conditions?: AgentStatusCondition[];
  };
}

/**
 * The Agent CR draft a create request sends.
 *
 * The same shape as `Agent` minus the fields only the cluster can fill in, so a
 * form builds one of these and the controller answers with the full resource.
 */
export interface AgentCreateRequest {
  apiVersion?: string;
  kind?: string;
  metadata: ResourceMetadata;
  spec: AgentSpec;
}

/** The two agent kinds the controller serves, spelled as its own `Kind` strings. */
export type AgentKindName = "SandboxAgent" | "AgentHarness";

/**
 * Where an `AgentHarness` is actually running.
 *
 * Reported by `AgentService.ListAgents` for harness agents and absent for
 * everything else. `acpPath` is the harness's own protocol endpoint — ACP over
 * HTTP, not A2A — which is why a harness cannot be chatted with by the A2A client
 * this app ships.
 */
export interface AgentHarnessDetails {
  backend: string;
  actorId: string;
  backendRefId: string;
  endpoint: string;
  acpPath: string;
}

/**
 * One row of `AgentService.ListAgents`: the CR plus resolved, denormalised fields.
 *
 * `tools` and `memoryRefs` are guaranteed to be arrays here because
 * `normaliseAgentResponse` makes them ones — neither the wire nor a hand-written
 * fake guarantees that. See below.
 */
export interface AgentResponse {
  id: number | string;
  /**
   * The custom resource itself.
   *
   * ## This type is currently wider than what arrives
   *
   * The controller serves `SandboxAgent` and `AgentHarness` under
   * `kagent.dev/v1alpha3`. `Agent` below still describes the `v1alpha2` CRD this
   * UI was written against, and the two agree only on `apiVersion`, `kind` and
   * `metadata` — which is why lists, tables and links work while anything reading
   * `spec.declarative` or `spec.byo` reads `undefined`. Re-modelling the domain on
   * the three v1alpha3 kinds is its own piece of work; until it lands, treat
   * `spec` as unverified rather than as described.
   */
  agent: Agent;
  model: string;
  modelProvider: string;
  modelConfigRef: string;
  tools: Tool[];
  /** Memory resources the agent is wired to, as `namespace/name` refs. */
  memoryRefs: string[];
  deploymentReady: boolean;
  accepted: boolean;
  /**
   * Which resource this row is — and so which RPCs can act on it.
   *
   * The distinction the whole API is now split along. A `SandboxAgent` has Get,
   * Create, Update and Delete. An `AgentHarness` has Get, Create and Delete and
   * **no Update RPC at all**, so a form that offers to save one has nothing to
   * call. `"unknown"` is a kind this build has not seen, which is not something to
   * assume anything about.
   */
  agentKind: AgentKindName | "unknown";
  /** Set for an `AgentHarness`: where its runtime lives. */
  agentHarness?: AgentHarnessDetails;
}

/**
 * Whether there is a resource behind this row that can be written to.
 *
 * `AgentService` has an `UpdateSandboxAgent` and nothing equivalent for a
 * harness, so a `SandboxAgent` is editable and an `AgentHarness` is not. A form
 * that offers to save one has no RPC to call, which reads to the person using it
 * as the save being broken rather than as the agent not being editable.
 *
 * Deliberately a positive check on the kind we know is writable rather than
 * `!== "AgentHarness"`: a kind this build has not seen before is not something to
 * assume is safe to write.
 */
export function hasAgentResource(row: AgentResponse): boolean {
  return row.agentKind === "SandboxAgent";
}

/**
 * Whether a conversation with this agent goes to the sandbox A2A path.
 *
 * The controller registers an A2A handler for `SandboxAgent`s only, and registers
 * it under `sandboxes/{namespace}/{name}` — which is what `/api/a2a-sandboxes/`
 * serves (`a2a_registrar.go`, `a2a_handler_mux.go`). So for a sandbox agent the
 * answer is yes, and for anything else there is no A2A handler to reach on either
 * path.
 *
 * Worth stating because this reads as the opposite of what it replaced. Under the
 * previous API a "sandbox" was the agent *without* a resource, so the chat page
 * asked for the sandbox path when the agent could not be edited. Now the sandbox
 * agent is the ordinary, editable one.
 */
export function usesSandboxA2APath(row: AgentResponse): boolean {
  return row.agentKind === "SandboxAgent";
}

/**
 * The same row as it may actually arrive.
 *
 * Proto3 has no absent repeated field, so the live path always yields an array —
 * but an operation override or a hand-written fake can still hand over
 * `undefined`, and the REST API this replaced sent `null` (Go marshals a nil
 * slice that way). An agent with no tools is the ordinary case, not an edge one.
 */
type WireAgentResponse = Omit<AgentResponse, "tools" | "memoryRefs"> & {
  tools?: Tool[] | null;
  memoryRefs?: string[] | null;
};

/**
 * Fills in the collections the wire may leave empty, so nothing above the data
 * layer has to know that absent and empty mean the same thing here.
 *
 * Normalised at the boundary rather than defended against at each use. Every
 * render site would otherwise need its own `?? []`, the declared type would be a
 * lie in the meantime, and the one place that forgot would throw — which is
 * precisely what happened: a `tools.length` in a table column took the whole
 * agents page down against a real cluster, on the three agents out of twelve that
 * happened to have no tools.
 */
export function normaliseAgentResponse(raw: WireAgentResponse): AgentResponse {
  return { ...raw, tools: raw.tools ?? [], memoryRefs: raw.memoryRefs ?? [] };
}
