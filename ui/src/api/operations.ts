import type { Client } from "@connectrpc/connect";
import type { ScheduledRunService } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";
/**
 * Every call the UI knows how to make, behind a stable id.
 *
 * This replaces the path table this file's predecessor held. The controller no
 * longer serves the application API over REST — it is gRPC, wrapped as gRPC-Web
 * (`grpcserver.WebHandler`, routed in `go/core/internal/httpserver/server.go`) —
 * so there is no longer a path to name. What is left to name is the *operation*:
 * "list model configs", "create a model config". An id per operation is what the
 * rest of the app depends on, and it is what survived.
 *
 * Nothing above this file addresses a service or a method. Going through ids
 * means a deployment can replace one operation's implementation (see
 * `registerOperationOverride` in `extensionPoints`) without any caller changing,
 * and the mock backend can register fakes from the same table the real client
 * builds calls from.
 *
 * ## One id per operation, never shared
 *
 * Kept from the path table, and for the same reason: the id — not the RPC — is
 * what an override is keyed by, so sharing one between a list read and a create
 * would mean an extension re-pointing a list silently re-pointed creation with it,
 * with no way to override one alone. Two ids resolving to the
 * same RPC is the cost of keeping those two things separately addressable.
 *
 * ## Where the RPCs are
 *
 * The default implementation of each id lives in `./grpc/operations.ts`, next to
 * the conversion between the proto messages and this app's domain types. The
 * mapping from id to RPC is documented there, including the four places where it
 * is not one-to-one.
 */

import { getOperationOverride } from "./extensionPoints";
import { defaultOperations } from "./grpc/operations";
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
import type {
  AgentInstance,
  AgentInstanceShare,
  AgentInstanceSharePermission,
  CreatedAgentInstanceShare,
} from "./domain/agentInstances";
import type {
  Checkpoint,
  CheckpointPage,
  CheckpointSort,
} from "./domain/checkpoints";
import type { Harness, HarnessResource } from "./domain/harnesses";
import type {
  AgentTemplate,
  AgentTemplateResource,
} from "./domain/agentTemplates";

/** An operation that takes nothing. Written `{}` at the call site. */
export type NoInput = Record<string, never>;

/** A namespaced Kubernetes resource. */
export interface ResourceRefInput {
  namespace: string;
  name: string;
}

/** A database-backed conversation, identified by UUID. */
export interface AgentInstanceRef {
  id: string;
}

/** Which direction a paged substrate read is sorted in. */
export type SubstrateSortOrder = "asc" | "desc";

/** The columns `substrate.actors` can order by. */
export type SubstrateActorSortField =
  /** Groups by status and orders by id within each group. The default. */
  | "default"
  | "status"
  | "actorId"
  | "template"
  | "workerPod";

/** The columns `substrate.workers` can order by. */
export type SubstrateWorkerSortField =
  /** Groups by pool and orders by pod within each group. The default. */
  | "default"
  | "pool"
  | "pod"
  | "actor";

/** What a paged, filtered substrate read takes. */
export interface SubstratePageInput<Sort = string> {
  namespace?: string;
  /** Matched server-side against the fields the row displays. Empty matches everything. */
  filter?: string;
  /** Rows per page. The controller refuses anything over 100 rather than clamping. */
  limit?: number;
  /** Empty for the first page; otherwise the previous response's `nextPageToken`. */
  pageToken?: string;
  /**
   * Which column to order by, and in which direction.
   *
   * Sent rather than applied here, for the same reason the filter is: the rows are
   * one page of hundreds of thousands, so ordering them locally reorders the page
   * rather than the result — which looks like sorting and is not.
   */
  sortField?: Sort;
  sortOrder?: SubstrateSortOrder;
}

/**
 * The input and output of every operation, keyed by id.
 *
 * Inputs are objects rather than positional arguments so that an override, a
 * transform and a fake all see the same named fields as the implementation — a
 * positional signature cannot be inspected by any of them.
 */
type ScheduledRunRpc<K extends keyof Client<typeof ScheduledRunService>> = {
  input: Parameters<Client<typeof ScheduledRunService>[K]>[0];
  output: Awaited<ReturnType<Client<typeof ScheduledRunService>[K]>>;
};

export interface OperationMap {
  "scheduledRuns.list": ScheduledRunRpc<"listScheduledRuns">;
  "scheduledRuns.get": ScheduledRunRpc<"getScheduledRun">;
  "scheduledRuns.create": ScheduledRunRpc<"createScheduledRun">;
  "scheduledRuns.update": ScheduledRunRpc<"updateScheduledRun">;
  "scheduledRuns.delete": ScheduledRunRpc<"deleteScheduledRun">;
  "scheduledRuns.trigger": ScheduledRunRpc<"triggerScheduledRun">;
  "scheduledRuns.executions": ScheduledRunRpc<"listScheduledRunExecutions">;
  "models.list": { input: NoInput; output: ModelConfig[] };
  "models.get": { input: ResourceRefInput; output: ModelConfig };
  "models.create": { input: { payload: CreateModelConfigRequest }; output: ModelConfig };
  "models.update": {
    input: ResourceRefInput & { payload: CreateModelConfigRequest };
    output: ModelConfig;
  };
  "models.delete": { input: ResourceRefInput; output: void };
  "models.providers": { input: NoInput; output: Provider[] };
  "models.providerModels": { input: NoInput; output: ProviderModelsResponse };

  "mcpServers.list": { input: NoInput; output: ToolServerResponse[] };
  "mcpServers.create": {
    input: { payload: ToolServerCreateRequest };
    output: ToolServerResponse;
  };
  "mcpServers.delete": { input: ResourceRefInput; output: void };
  "tools.list": { input: NoInput; output: ToolsResponse[] };

  "prompts.list": { input: { namespace?: string }; output: PromptTemplateSummary[] };
  "prompts.get": { input: ResourceRefInput; output: PromptTemplateDetail };
  "prompts.create": {
    input: { payload: CreatePromptTemplateRequest };
    output: PromptTemplateDetail;
  };
  "prompts.update": {
    input: ResourceRefInput & { payload: UpdatePromptTemplateRequest };
    output: PromptTemplateDetail;
  };
  "prompts.delete": { input: ResourceRefInput; output: void };

  /** All instances visible to the caller, optionally filtered by Kubernetes targets. */
  "agentInstances.list": {
    input: {
      allCreators?: boolean;

      agentTemplate?: ResourceRefInput;
      harness?: ResourceRefInput;
    };
    output: AgentInstance[];
  };
  "agentInstances.get": { input: AgentInstanceRef; output: AgentInstance };

  "agentInstances.create": {
    input: {
      harness: ResourceRefInput;
      agentTemplate: ResourceRefInput;
      requestId: string;

      /**
       * The reader's title for the conversation. Optional; empty means unnamed.
       *
       * Bounded and validated exactly as a rename is — `conversationNameProblem` in
       * `domain/agentInstances` is the controller's rule, so a caller can refuse
       * before the round trip.
       */
      name?: string;
    };
    output: AgentInstance;
  };

  /**
   * Retitles a conversation, answering with the record as it now stands.
   *
   * A write, unlike everything else on this service except create and delete: its
   * policy entry is `AccessUpdate`, so a read-only share cannot retitle a
   * conversation for everyone holding the link. Scoped to the creator like every
   * other instance read, so somebody else's conversation cannot be renamed.
   *
   * An empty name clears the title rather than being rejected.
   */
  "agentInstances.rename": {
    input: AgentInstanceRef & { name: string };
    output: AgentInstance;
  };

  /**
   * Forks a conversation: a new instance that starts from where this one is now.
   *
   * Two controller calls, not one: a checkpoint of the source at its current turn
   * boundary, then a fork of that checkpoint. The source has to be quiescent, so a
   * conversation mid-turn is refused with `FailedPrecondition`. The fork comes back
   * unnamed; pass `name` to title it in the same operation.
   */
  "agentInstances.fork": {
    input: AgentInstanceRef & { requestId: string; name?: string };
    output: AgentInstance;
  };

  /**
   * Saves the conversation's current turn boundary, so a fork can start from it later.
   *
   * The controller has no cutoff to offer: what is saved is wherever the conversation
   * stands now. A conversation mid-turn has no boundary to save and is refused with
   * `FailedPrecondition`.
   */
  "agentInstances.checkpoints.create": {
    input: AgentInstanceRef & { requestId: string };
    output: Checkpoint;
  };

  /**
   * Saved boundaries, narrowed, ordered and paged by the controller. Without an `id`,
   * every boundary the caller owns; `filter` matches the recorded conversation name and
   * the checkpoint's identifiers, and `sort` applies in the order given.
   */
  "agentInstances.checkpoints.list": {
    input: {
      id?: string;
      filter?: string;
      sort?: readonly CheckpointSort[];
      limit?: number;
      offset?: number;
    };
    output: CheckpointPage;
  };

  /**
   * Removes a saved boundary, and with it the snapshot it was holding.
   *
   * A checkpoint pins a copy of the conversation's runtime in the substrate — that is
   * what makes forking one possible — so this is the only thing that gives that space
   * back. Forks already made from it are unaffected: they own their own copy.
   */
  "agentInstances.checkpoints.delete": {
    input: { checkpointId: string };
    output: void;
  };

  /**
   * Forks a saved boundary: a new conversation holding the transcript up to it.
   *
   * Unlike `agentInstances.fork` this starts from a boundary saved earlier, so the
   * fork's history stops there rather than at the source's latest turn. The fork
   * comes back unnamed; pass `name` to title it in the same operation.
   */
  "agentInstances.checkpoints.fork": {
    input: { checkpointId: string; requestId: string; name?: string };
    output: AgentInstance;
  };

  /**
   * Deletes an instance.
   *
   * Irreversible, and it takes the conversation with it: the instance *is* the
   * conversation, so its tasks go too. Every caller confirms first.
   */
  "agentInstances.delete": { input: AgentInstanceRef; output: void };

  "agentInstances.shares.list": {
    input: AgentInstanceRef;
    output: AgentInstanceShare[];
  };
  "agentInstances.shares.create": {
    input: AgentInstanceRef & { permission: AgentInstanceSharePermission };
    output: CreatedAgentInstanceShare;
  };

  /** Revoked by share id, not by token: the token is not stored to match on. */
  "agentInstances.shares.revoke": {
    input: { shareId: string };
    output: void;
  };

  "agentInstances.suspend": { input: AgentInstanceRef; output: AgentInstance };
  "agentInstances.resume": { input: AgentInstanceRef; output: AgentInstance };

  "harnesses.list": { input: { namespace?: string }; output: Harness[] };
  /**
   * Creates a harness from a whole custom resource.
   *
   * `HarnessService` implements create, update and delete — a note in this codebase
   * said it was read-only, and that was wrong: it described what this client exposed
   * rather than what the service does.
   */
  "harnesses.create": {
    input: { namespace: string; name: string; resource: HarnessResource };
    output: Harness;
  };
  "harnesses.delete": { input: ResourceRefInput; output: void };
  /** The agent templates in one namespace, or in every observed namespace. */
  "agentTemplates.list": { input: { namespace?: string }; output: AgentTemplate[] };
  "agentTemplates.get": { input: ResourceRefInput; output: AgentTemplate };
  /**
   * Creates an agent template from a whole custom resource.
   *
   * The resource carries `metadata.labels`, and they are not decoration: a
   * `Harness` admits templates through a label selector, and the CRD says a harness
   * with no selector admits none. A template whose labels match nothing reaches no
   * prepared revision and can never become an agent.
   */
  "agentTemplates.create": {
    input: { namespace: string; name: string; resource: AgentTemplateResource };
    output: AgentTemplate;
  };
  /**
   * Replaces an agent template.
   *
   * Takes the whole resource, not a patch — so a caller that sends a spec built
   * only from the fields it displays deletes every field it does not model.
   * `specFromDraft` merges onto the existing spec for exactly this reason.
   */
  "agentTemplates.update": {
    input: { namespace: string; name: string; resource: AgentTemplateResource };
    output: AgentTemplate;
  };
  "agentTemplates.delete": { input: ResourceRefInput; output: void };

  "namespaces.list": { input: NoInput; output: NamespaceResponse[] };
  /**
   * The whole substrate inventory in one read.
   *
   * Kept for the small clusters where it still works, and used by nothing on
   * screen: it does not survive a real one. A deployment reporting 103,134 actors
   * answers with a message gRPC refuses to send — 43MB against a 16MB ceiling — so
   * the page that depended on it could not load at all. The three operations below
   * replaced it, and raising the ceiling would only move the number.
   */
  "substrate.status": {
    input: { namespace?: string };
    output: SubstrateStatusResponse;
  };
  /**
   * Counts, and the two lists small enough to travel whole.
   *
   * The only honest source of a total on the substrate page: every other read
   * there is a page, and a page counted and presented as a total would report
   * "20 actors" for a cluster running a hundred thousand.
   */
  "substrate.summary": {
    input: { namespace?: string };
    output: SubstrateSummary;
  };
  /**
   * One page of actors, narrowed server-side.
   *
   * The filter is sent rather than applied here, because filtering a page that has
   * already been fetched searches only what was fetched — a match on page nine
   * reads on screen as "no matches".
   */
  "substrate.actors": {
    input: SubstratePageInput<SubstrateActorSortField>;
    output: SubstrateActorPage;
  };
  /** One page of worker assignments. The mirror of `substrate.actors`. */
  "substrate.workers": {
    input: SubstratePageInput<SubstrateWorkerSortField>;
    output: SubstrateWorkerPage;
  };
}

export type OperationId = keyof OperationMap;
export type OperationInput<K extends OperationId> = OperationMap[K]["input"];
export type OperationOutput<K extends OperationId> = OperationMap[K]["output"];

/** Options every operation accepts, so callers can cancel in-flight work. */
export interface OperationCallOptions {
  signal?: AbortSignal;
}

export type ApiOperation<K extends OperationId> = (
  input: OperationInput<K>,
  options: OperationCallOptions,
) => Promise<OperationOutput<K>>;

export type ApiOperations = { [K in OperationId]: ApiOperation<K> };

/**
 * Every operation id, for callers that need to enumerate them.
 *
 * Derived from the implementation table rather than written out, so an operation
 * cannot exist without appearing here — which is what the mock backend and the
 * extension-point validation both rely on.
 */
export const operationIds = Object.keys(defaultOperations) as OperationId[];

/**
 * Runs an operation: the registered override if there is one, else the default.
 *
 * The one entry point. Request and response transforms are *not* applied here —
 * they are applied inside the transport, where the finished gRPC request exists
 * to be changed (see `transport.ts`). Doing it here would mean a transform could
 * only see this app's domain arguments and never the call itself.
 */
export function invoke<K extends OperationId>(
  id: K,
  input: OperationInput<K>,
  options: OperationCallOptions = {},
): Promise<OperationOutput<K>> {
  const override = getOperationOverride(id);
  const operation = override ?? (defaultOperations[id] as ApiOperation<K>);
  return operation(input, options);
}
