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

import { invoke } from "./operations";
import { sortedByFields, sortedByNamespaceThenName, sortedByRef } from "./order";
import type { AgentKindName } from "./domain/agents";
import type { Agent, AgentCreateRequest, AgentResponse } from "./domain/agents";
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

/** Options every read method accepts, so callers can cancel in-flight work. */
export interface ReadOptions {
  signal?: AbortSignal;
}

export interface AgentsApi {
  list(options?: ReadOptions): Promise<AgentResponse[]>;
  /**
   * One agent, by ref.
   *
   * `kind` is optional because a detail page reading a URL does not know it. Left
   * out, the sandbox agent is tried and the harness is tried on a 404 — one extra
   * round trip for a harness, and none for the common case. Pass it when the
   * caller already has the row.
   */
  get(
    namespace: string,
    name: string,
    options?: ReadOptions & { kind?: AgentKindName },
  ): Promise<AgentResponse>;
  /**
   * Creates an agent and returns the created resource.
   *
   * A bare custom resource — `{metadata, spec, status}` — not the wrapped row
   * `list` and `get` return. Which kind of resource is read from the draft's own
   * `kind`, defaulting to `SandboxAgent`; the controller has a separate create RPC
   * per kind and no way to infer one.
   */
  create(payload: AgentCreateRequest): Promise<Agent>;
  /**
   * Replaces an agent, returning the updated resource.
   *
   * Takes the whole resource, not a patch: the controller replaces the spec it is
   * given, so a caller that sends a partial agent silently drops whatever it left
   * out.
   *
   * Only a `SandboxAgent` can be updated — `AgentService` has no update operation
   * for an `AgentHarness`, and asking to update one rejects rather than sending a
   * request that cannot succeed.
   */
  update(payload: AgentCreateRequest): Promise<Agent>;
  /**
   * Deletes an agent.
   *
   * `kind` picks which delete RPC is used and defaults to `SandboxAgent`, the only
   * kind any screen currently offers to delete. A caller holding a harness row has
   * to say so.
   */
  remove(namespace: string, name: string, kind?: AgentKindName): Promise<void>;
}

export interface ModelsApi {
  list(options?: ReadOptions): Promise<ModelConfig[]>;
  get(namespace: string, name: string, options?: ReadOptions): Promise<ModelConfig>;
  /** Models on offer, grouped by provider name. */
  providerModels(options?: ReadOptions): Promise<ProviderModelsResponse>;
  /** Every provider the controller knows: the stock ones and the configured ones. */
  providers(options?: ReadOptions): Promise<Provider[]>;
  create(payload: CreateModelConfigRequest): Promise<ModelConfig>;
  /** Replaces a model configuration. Addressed per resource, unlike agents. */
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
   * `HarnessService`, not `AgentService`: `Harness` and `AgentHarness` are
   * different CRDs that share nothing but a name.
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
  /**
   * Every instance in one namespace, every page of it.
   *
   * The namespace is required rather than optional, unlike every other list here:
   * `AgentInstanceService` has no read that spans namespaces, and the controller
   * rejects an empty one as an invalid argument rather than treating it as "all".
   * A caller that wants several namespaces asks several times.
   *
   * `allCreators` widens the read to instances other people created. It is
   * authorised separately from the list itself, so it can be refused where the
   * plain read succeeds — which is a message to show, not a reason to hide the
   * control.
   */
  list(
    namespace: string,
    options?: ReadOptions & {
      allCreators?: boolean;
      /**
       * One agent's conversations. Bare names within `namespace`; either alone is
       * a valid narrowing, and the controller does it server-side.
       */
      agentTemplate?: string;
      harness?: string;
    },
  ): Promise<AgentInstance[]>;
  get(namespace: string, id: string, options?: ReadOptions): Promise<AgentInstance>;
  /**
   * Suspends a running instance, answering with the record as it now stands.
   *
   * Only a `ready` instance with no operation in flight can be suspended; the
   * controller refuses anything else with `Aborted`. `canSuspend` in
   * `domain/agentInstances` is that precondition, so a caller can ask before it
   * offers the action.
   */
  suspend(namespace: string, id: string): Promise<AgentInstance>;
  /** Resumes a suspended instance. The mirror of `suspend`, and `canResume` guards it. */
  resume(namespace: string, id: string): Promise<AgentInstance>;
  /**
   * Creates an instance from a harness and a template.
   *
   * Two names and a namespace is the whole request: what the agent *is* lives on
   * the template and how it *runs* lives on the harness, so this is a choice of
   * pair rather than a spec. The controller refuses a pair it does not admit, and
   * one whose prepared revision is not ready, with `FailedPrecondition`.
   */
  create(input: {
    namespace: string;
    harness: string;
    agentTemplate: string;
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
  rename(namespace: string, id: string, name: string): Promise<AgentInstance>;
  /**
   * Deletes an instance, and with it the conversation held against it.
   *
   * The instance *is* the conversation, so this is not a tidy-up — it removes what
   * was said. Every caller confirms first.
   */
  remove(namespace: string, id: string): Promise<void>;
  /**
   * Share links over one instance.
   *
   * The instance *is* the conversation, so a share hands somebody what was said.
   * The token comes back only from `create` — the controller stores its digest —
   * so a caller that discards it cannot show it again.
   */
  shares: {
    list(namespace: string, id: string, options?: ReadOptions): Promise<AgentInstanceShare[]>;
    create(
      namespace: string,
      id: string,
      permission: AgentInstanceSharePermission,
    ): Promise<CreatedAgentInstanceShare>;
    /** By share id, not by token: the token is not stored to match on. */
    revoke(namespace: string, shareId: string): Promise<void>;
  };
}

export interface KagentApiClient {
  agents: AgentsApi;
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
    agents: {
      list: (options) =>
        invoke("agents.list", {}, options).then((rows) =>
          sortedByNamespaceThenName(rows, (row) => ({
            namespace: row.agent.metadata?.namespace ?? "",
            name: row.agent.metadata?.name ?? "",
          })),
        ),
      get: (namespace, name, options) =>
        invoke("agents.get", { namespace, name, kind: options?.kind }, options),
      create: (payload) => invoke("agents.create", { resource: payload }),
      update: (payload) => invoke("agents.update", { resource: payload }),
      remove: (namespace, name, kind) =>
        invoke("agents.delete", { namespace, name, kind }),
    },

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
      /*
       * Sorted by namespace then id — a stable order rather than a meaningful one,
       * and stable is the point: the rows do not rearrange between reads.
       *
       * Deliberately *not* sorted by name, even though a conversation has one now.
       * Most conversations are unnamed, so a name sort would put every untitled one
       * in a block whose internal order changed as titles were added. The surfaces
       * that want a meaningful order say so with a column the reader chose; when
       * they do not, this is what they get.
       */
      list: (namespace, options) =>
        invoke(
          "agentInstances.list",
          {
            namespace,
            allCreators: options?.allCreators,
            agentTemplate: options?.agentTemplate,
            harness: options?.harness,
          },
          options,
        ).then((rows) =>
          sortedByNamespaceThenName(rows, (row) => ({
            namespace: row.namespace,
            name: row.id,
          })),
        ),
      rename: (namespace, id, name) =>
        invoke("agentInstances.rename", { namespace, id, name }),
      get: (namespace, id, options) =>
        invoke("agentInstances.get", { namespace, id }, options),
      suspend: (namespace, id) => invoke("agentInstances.suspend", { namespace, id }),
      resume: (namespace, id) => invoke("agentInstances.resume", { namespace, id }),
      create: (input) => invoke("agentInstances.create", input),
      remove: (namespace, id) => invoke("agentInstances.delete", { namespace, id }),
      shares: {
        list: (namespace, id, options) =>
          invoke("agentInstances.shares.list", { namespace, id }, options),
        create: (namespace, id, permission) =>
          invoke("agentInstances.shares.create", { namespace, id, permission }),
        revoke: (namespace, shareId) =>
          invoke("agentInstances.shares.revoke", { namespace, shareId }),
      },
    },
  };
}

/** The client the app uses. Swapping backends never touches this object. */
export const apiClient: KagentApiClient = createApiClient();
