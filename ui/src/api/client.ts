/**
 * The typed API surface the rest of the app calls.
 *
 * Methods take domain arguments and return domain models — the proto messages,
 * the `StructuredObject` envelope, the RPC names and the mock/live decision all
 * stop here. Hooks depend on the `KagentApiClient` interface rather than the
 * concrete object, so a test can substitute a hand-written implementation without
 * a transport at all.
 *
 * Every method is one call to `invoke`, which is where an operation override gets
 * its chance. That indirection is the point: this file describes *what* the app
 * can ask for, and `grpc/operations.ts` decides *how*.
 */

import { type ResourceRefInput, invoke } from "./operations";
import { sortedByFields, sortedByRef } from "./order";
import type {
  CreateModelConfigRequest,
  ModelConfig,
  Provider,
  ProviderModelsResponse,
} from "./domain/models";
import type {
  ToolServerCreateRequest,
  ToolServerResponse,
  ToolsResponse,
} from "./domain/mcpServers";
import type {
  CreatePromptTemplateRequest,
  PromptTemplateDetail,
  PromptTemplateSummary,
  UpdatePromptTemplateRequest,
} from "./domain/prompts";
import type { NamespaceResponse } from "./domain/namespaces";
import type {
  SubstrateActorPage,
  SubstrateStatusResponse,
  SubstrateSummary,
  SubstrateWorkerPage,
} from "./domain/substrate";
import type { Harness, HarnessResource } from "./domain/harnesses";
import type {
  AgentTemplate,
  AgentTemplateResource,
} from "./domain/agentTemplates";
import type {
  SubstrateActorSortField,
  SubstratePageInput,
  SubstrateWorkerSortField,
} from "./operations";
import type {
  AgentInstance,
  AgentInstanceShare,
  AgentInstanceSharePermission,
  CreatedAgentInstanceShare,
} from "./domain/agentInstances";
import type { Checkpoint, CheckpointPage, CheckpointSort } from "./domain/checkpoints";

/** Options every read method accepts, so callers can cancel in-flight work. */
export interface ReadOptions {
  signal?: AbortSignal;
}

export interface ModelsApi {
  list(options?: ReadOptions): Promise<ModelConfig[]>;
  get(namespace: string, name: string, options?: ReadOptions): Promise<ModelConfig>;
  /** Models on offer, grouped by provider name. */
  providerModels(options?: ReadOptions): Promise<ProviderModelsResponse>;
  /** Every provider the controller knows: the stock ones and the configured ones. */
  providers(options?: ReadOptions): Promise<Provider[]>;
  create(payload: CreateModelConfigRequest): Promise<ModelConfig>;
  /** Replaces a model configuration. */
  update(
    namespace: string,
    name: string,
    payload: CreateModelConfigRequest,
  ): Promise<ModelConfig>;
  remove(namespace: string, name: string): Promise<void>;
}

export interface McpServersApi {
  list(options?: ReadOptions): Promise<ToolServerResponse[]>;
  /** Every discovered tool, flattened across servers. */
  tools(options?: ReadOptions): Promise<ToolsResponse[]>;
  create(payload: ToolServerCreateRequest): Promise<ToolServerResponse>;
  remove(namespace: string, name: string): Promise<void>;
}

export interface PromptsApi {
  list(namespace?: string, options?: ReadOptions): Promise<PromptTemplateSummary[]>;
  get(
    namespace: string,
    name: string,
    options?: ReadOptions,
  ): Promise<PromptTemplateDetail>;
  create(payload: CreatePromptTemplateRequest): Promise<PromptTemplateDetail>;
  update(
    namespace: string,
    name: string,
    payload: UpdatePromptTemplateRequest,
  ): Promise<PromptTemplateDetail>;
  remove(namespace: string, name: string): Promise<void>;
}

export interface NamespacesApi {
  list(options?: ReadOptions): Promise<NamespaceResponse[]>;
}

export interface SubstrateApi {
  /**
   * The whole inventory in one read, optionally narrowed to one namespace.
   *
   * Does not survive a large cluster and is used by nothing on screen — see the
   * operation's own note. `summary`, `actors` and `workers` are what the substrate
   * page reads.
   */
  status(namespace?: string, options?: ReadOptions): Promise<SubstrateStatusResponse>;
  /** Counts and the two small lists. The only honest source of a total. */
  summary(namespace?: string, options?: ReadOptions): Promise<SubstrateSummary>;
  /** One page of actors, narrowed and ordered server-side. */
  actors(
    input: SubstratePageInput<SubstrateActorSortField>,
    options?: ReadOptions,
  ): Promise<SubstrateActorPage>;
  /** One page of worker assignments, narrowed and ordered server-side. */
  workers(
    input: SubstratePageInput<SubstrateWorkerSortField>,
    options?: ReadOptions,
  ): Promise<SubstrateWorkerPage>;
}

/** The two halves an AgentInstance is created from. */
export interface AgentBuildingBlocksApi {
  /**
   * Every `Harness` — the runtime half — in one namespace, or in all of them.
   *
   * Served by `HarnessService`; a Harness is the reusable runtime half of an agent.
   */
  harnesses(namespace?: string, options?: ReadOptions): Promise<Harness[]>;
  /**
   * Creates a harness.
   *
   * `HarnessService` implements create, update and delete. A note in this codebase said
   * it was read-only, which described what this client exposed rather than what the
   * service does — the RPCs were there and implemented the whole time.
   */
  createHarness(input: {
    namespace: string;
    name: string;
    resource: HarnessResource;
  }): Promise<Harness>;
  /** Deletes a harness. Templates admitted only by it then run nowhere. */
  removeHarness(namespace: string, name: string): Promise<void>;
  /** Every `AgentTemplate` — the behaviour half — in one namespace, or in all of them. */
  agentTemplates(namespace?: string, options?: ReadOptions): Promise<AgentTemplate[]>;
  /** One agent template, whole, as an edit form needs it. */
  agentTemplate(
    namespace: string,
    name: string,
    options?: ReadOptions,
  ): Promise<AgentTemplate>;
  /**
   * Creates an agent template from a whole custom resource.
   *
   * `metadata.labels` ride with it and decide whether any harness will run it —
   * see `domain/agentTemplates`.
   */
  createAgentTemplate(input: {
    namespace: string;
    name: string;
    resource: AgentTemplateResource;
  }): Promise<AgentTemplate>;
  /**
   * Replaces an agent template.
   *
   * Whole-resource, so a caller sending a spec built only from what it displays
   * deletes every field it does not model. `specFromDraft` merges for that reason.
   */
  updateAgentTemplate(input: {
    namespace: string;
    name: string;
    resource: AgentTemplateResource;
  }): Promise<AgentTemplate>;
  removeAgentTemplate(namespace: string, name: string): Promise<void>;
}

export interface AgentInstancesApi {
  list(
    options?: ReadOptions & {
      allCreators?: boolean;
      agentTemplate?: ResourceRefInput;
      harness?: ResourceRefInput;
    },
  ): Promise<AgentInstance[]>;
  get(id: string, options?: ReadOptions): Promise<AgentInstance>;

  /**
   * Suspends a running instance, answering with the record as it now stands.
   *
   * Only a `ready` instance with no operation in flight can be suspended; the
   * controller refuses anything else with `Aborted`. `canSuspend` in
   * `domain/agentInstances` is that precondition, so a caller can ask before it
   * offers the action.
   */
  suspend(id: string): Promise<AgentInstance>;

  /** Resumes a suspended instance. The mirror of `suspend`, and `canResume` guards it. */
  resume(id: string): Promise<AgentInstance>;

  create(input: {
    harness: ResourceRefInput;
    agentTemplate: ResourceRefInput;
    /**
     * The controller's idempotency key. Required — blank is `InvalidArgument`.
     *
     * A caller retrying a failed create sends the same value, so the retry cannot
     * produce a second instance.
     */
    requestId: string;
    /** The reader's title for the conversation. Omit it to leave it unnamed. */
    name?: string;
  }): Promise<AgentInstance>;

  /**
   * Retitles a conversation, answering with the record as it now stands.
   *
   * The only write here that is not a lifecycle operation, and it authorises as one
   * — so a read-only share link cannot retitle a conversation for everyone holding
   * it. An empty name clears the title. `conversationNameProblem` in
   * `domain/agentInstances` is the controller's validation, copied, so a caller can
   * refuse before the round trip.
   */
  rename(id: string, name: string): Promise<AgentInstance>;

  /**
   * Forks a conversation into a new one that starts from its current state.
   *
   * The source must be idle between turns. Pass `name` to title the fork; the
   * controller otherwise leaves it unnamed.
   */
  fork(id: string, name?: string): Promise<AgentInstance>;

  /**
   * The turn boundaries a fork can start from.
   *
   * `create` saves the conversation where it stands — there is no cutoff to pass, so
   * a boundary further back is one that was saved while it was the latest. `fork`
   * then starts a conversation holding the history up to whichever one is named.
   */
  checkpoints: {
    /**
     * One page of saved boundaries, narrowed and ordered by the controller. `id`
     * narrows to one conversation; without it, every boundary the caller owns.
     */
    list(
      input?: {
        id?: string;
        filter?: string;
        sort?: readonly CheckpointSort[];
        limit?: number;
        offset?: number;
      },
      options?: ReadOptions,
    ): Promise<CheckpointPage>;
    create(id: string): Promise<Checkpoint>;
    fork(checkpointId: string, name?: string): Promise<AgentInstance>;
    /** Releases the snapshot a boundary was holding. Forks already made keep theirs. */
    remove(checkpointId: string): Promise<void>;
  };

  /**
   * Deletes an instance, and with it the conversation held against it.
   *
   * The instance *is* the conversation, so this is not a tidy-up — it removes what
   * was said. Every caller confirms first.
   */
  remove(id: string): Promise<void>;

  shares: {
    list(id: string, options?: ReadOptions): Promise<AgentInstanceShare[]>;
    create(
      id: string,
      permission: AgentInstanceSharePermission,
    ): Promise<CreatedAgentInstanceShare>;

    /** By share id, not by token: the token is not stored to match on. */
    revoke(shareId: string): Promise<void>;
  };
}

export interface KagentApiClient {
  models: ModelsApi;
  mcpServers: McpServersApi;
  prompts: PromptsApi;
  namespaces: NamespacesApi;
  substrate: SubstrateApi;
  agentInstances: AgentInstancesApi;
  agentBuildingBlocks: AgentBuildingBlocksApi;
}

export function createApiClient(): KagentApiClient {
  return {
    models: {
      list: (options) => invoke("models.list", {}, options).then(sortedByRef),
      get: (namespace, name, options) =>
        invoke("models.get", { namespace, name }, options),
      providerModels: (options) => invoke("models.providerModels", {}, options),
      providers: (options) => invoke("models.providers", {}, options),
      create: (payload) => invoke("models.create", { payload }),
      update: (namespace, name, payload) =>
        invoke("models.update", { namespace, name, payload }),
      remove: (namespace, name) => invoke("models.delete", { namespace, name }),
    },

    mcpServers: {
      list: (options) => invoke("mcpServers.list", {}, options).then(sortedByRef),
      tools: (options) => invoke("tools.list", {}, options),
      create: (payload) => invoke("mcpServers.create", { payload }),
      remove: (namespace, name) => invoke("mcpServers.delete", { namespace, name }),
    },

    prompts: {
      list: (namespace, options) =>
        // Sorted per namespace here, and again after the fan-out that "all
        // namespaces" performs — one call cannot order rows it never saw.
        invoke("prompts.list", { namespace }, options).then(sortedByFields),
      get: (namespace, name, options) =>
        invoke("prompts.get", { namespace, name }, options),
      create: (payload) => invoke("prompts.create", { payload }),
      update: (namespace, name, payload) =>
        invoke("prompts.update", { namespace, name, payload }),
      remove: (namespace, name) => invoke("prompts.delete", { namespace, name }),
    },

    namespaces: {
      list: (options) => invoke("namespaces.list", {}, options),
    },

    substrate: {
      status: (namespace, options) =>
        invoke("substrate.status", { namespace }, options),
      summary: (namespace, options) =>
        invoke("substrate.summary", { namespace }, options),
      // Not sorted here, unlike every other list: the server orders these pages,
      // and re-sorting a page would order it within itself while leaving it in the
      // wrong place in the whole — which reads as a list that shuffles as you page.
      actors: (input, options) => invoke("substrate.actors", input, options),
      workers: (input, options) => invoke("substrate.workers", input, options),
    },

    agentBuildingBlocks: {
      harnesses: (namespace, options) =>
        invoke("harnesses.list", { namespace }, options).then(sortedByRef),
      createHarness: (input) => invoke("harnesses.create", input),
      removeHarness: (namespace, name) => invoke("harnesses.delete", { namespace, name }),
      agentTemplates: (namespace, options) =>
        invoke("agentTemplates.list", { namespace }, options).then(sortedByRef),
      agentTemplate: (namespace, name, options) =>
        invoke("agentTemplates.get", { namespace, name }, options),
      createAgentTemplate: (input) => invoke("agentTemplates.create", input),
      updateAgentTemplate: (input) => invoke("agentTemplates.update", input),
      removeAgentTemplate: (namespace, name) =>
        invoke("agentTemplates.delete", { namespace, name }),
    },

    agentInstances: {

      list: (options) =>
        invoke(
          "agentInstances.list",
          {
            allCreators: options?.allCreators,
            agentTemplate: options?.agentTemplate,
            harness: options?.harness,
          },
          options,
        ).then((rows) => rows.sort((a, b) => a.id.localeCompare(b.id))),
      rename: (id, name) =>
        invoke("agentInstances.rename", { id, name }),
      get: (id, options) =>
        invoke("agentInstances.get", { id }, options),
      suspend: (id) => invoke("agentInstances.suspend", { id }),
      resume: (id) => invoke("agentInstances.resume", { id }),
      create: (input) => invoke("agentInstances.create", input),
      remove: (id) => invoke("agentInstances.delete", { id }),
      fork: (id, name) =>
        invoke("agentInstances.fork", { id, requestId: crypto.randomUUID(), name }),
      checkpoints: {
        list: (input, options) =>
          invoke("agentInstances.checkpoints.list", input ?? {}, options),
        create: (id) =>
          invoke("agentInstances.checkpoints.create", {
            id,
            requestId: crypto.randomUUID(),
          }),
        fork: (checkpointId, name) =>
          invoke("agentInstances.checkpoints.fork", {
            checkpointId,
            requestId: crypto.randomUUID(),
            name,
          }),
        remove: (checkpointId) =>
          invoke("agentInstances.checkpoints.delete", { checkpointId }),
      },
      shares: {
        list: (id, options) =>
          invoke("agentInstances.shares.list", { id }, options),
        create: (id, permission) =>
          invoke("agentInstances.shares.create", { id, permission }),
        revoke: (shareId) =>
          invoke("agentInstances.shares.revoke", { shareId }),
      },
    },
  };
}

/** The client the app uses. Swapping backends never touches this object. */
export const apiClient: KagentApiClient = createApiClient();
