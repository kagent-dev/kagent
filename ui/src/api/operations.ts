/**
 * Every call the UI knows how to make, behind a stable id.
 *
 * This replaces the path table this file's predecessor held. The controller no
 * longer serves the application API over REST — it is gRPC, wrapped as gRPC-Web
 * (`grpcserver.WebHandler`, routed in `go/core/internal/httpserver/server.go`) —
 * so there is no longer a path to name. What is left to name is the *operation*:
 * "list the agents", "create a model config". An id per operation is what the
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
 * would mean an extension re-pointing its agent list silently re-pointed agent
 * creation with it, with no way to override one alone. Two ids resolving to the
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
import type {
  AgentInstance,
  AgentInstanceShare,
  AgentInstanceSharePermission,
  CreatedAgentInstanceShare,
} from "./domain/agentInstances";
import type { Harness, HarnessResource } from "./domain/harnesses";
import type {
  AgentTemplate,
  AgentTemplateResource,
} from "./domain/agentTemplates";

/** An operation that takes nothing. Written `{}` at the call site. */
export type NoInput = Record<string, never>;

/**
 * Which agent, and — where it matters — which kind of agent.
 *
 * The kind is part of the identity now, not a detail of it: `SandboxAgent` and
 * `AgentHarness` are two different resources served by two different RPCs, and
 * `AgentService` has no operation that reads "an agent" without knowing which.
 * A caller that genuinely does not know (a detail page holding only a URL) omits
 * it, and `agents.get` tries both — see its implementation.
 */
export interface AgentRef {
  namespace: string;
  name: string;
  kind?: AgentKindName;
}

/** The two agent kinds, spelled as the controller's own `Kind` strings. */
export type AgentKindName = "SandboxAgent" | "AgentHarness";

export interface ResourceRefInput {
  namespace: string;
  name: string;
}

/**
 * Which agent instance, and where.
 *
 * The namespace is not optional and not a filter: `AgentInstanceService` addresses
 * every instance as `(namespace, id)`, and `validateIdentity` on the controller
 * rejects a request whose namespace is not a DNS-1123 label — so an empty one is an
 * `InvalidArgument`, never "any namespace".
 */
export interface AgentInstanceRef {
  namespace: string;
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
export interface OperationMap {
  "agents.list": { input: { namespace?: string }; output: AgentResponse[] };
  "agents.get": { input: AgentRef; output: AgentResponse };
  "agents.create": { input: { resource: AgentCreateRequest }; output: Agent };
  "agents.update": { input: { resource: AgentCreateRequest }; output: Agent };
  "agents.delete": { input: AgentRef; output: void };

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

  /**
   * Every agent instance in one namespace.
   *
   * `namespace` is required for the reason `AgentInstanceRef` gives: there is no
   * "all namespaces" read on this service. `allCreators` asks for other people's
   * instances as well as the caller's, which the controller authorises separately —
   * so it can fail with `PermissionDenied` where the same call without it succeeds.
   */
  "agentInstances.list": {
    input: {
      namespace: string;
      allCreators?: boolean;
      /**
       * One agent's conversations, narrowed by the server.
       *
       * Both are bare names within `namespace`, both optional, and either may be
       * given alone. The controller matches them against the `(AgentTemplate,
       * Harness)` pair the instance's prepared revision was built from, so they
       * select instances stored before these fields existed.
       *
       * Narrowing here rather than in the browser is the point: this list is paged,
       * and filtering a page after fetching it reports "no conversations" about a
       * row on page nine.
       */
      agentTemplate?: string;
      harness?: string;
    };
    output: AgentInstance[];
  };
  "agentInstances.get": { input: AgentInstanceRef; output: AgentInstance };
  /**
   * Creates an instance from a harness and a template.
   *
   * The whole request is those two names and a namespace — there is no spec here.
   * That is the model rather than a simplification: what an agent *is* belongs to
   * the `AgentTemplate` and how it *runs* belongs to the `Harness`, so creating one
   * is choosing a pair.
   *
   * The pair has to be one the controller admits, and it must have reached a ready
   * prepared revision; anything else is `FailedPrecondition`. `admitsHarness` in
   * `domain/agentTemplates` is the first of those checks, so a picker can refuse
   * before asking.
   *
   * `requestId` is the controller's idempotency key and is **required** — an absent
   * or blank one is `InvalidArgument`, not a default. A caller retrying a create that
   * failed should send the same id, so the retry cannot produce a second instance.
   */
  "agentInstances.create": {
    input: {
      namespace: string;
      harness: string;
      agentTemplate: string;
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
   * Deletes an instance.
   *
   * Irreversible, and it takes the conversation with it: the instance *is* the
   * conversation, so its tasks go too. Every caller confirms first.
   */
  "agentInstances.delete": { input: AgentInstanceRef; output: void };
  /**
   * Suspends and resumes, each answering with the instance as it now stands.
   *
   * Two ids rather than one taking a direction, because they are two RPCs and
   * because an override should be able to re-point one without the other — the
   * same reason a list and a create are never one id here.
   */
  /**
   * Share links over one instance.
   *
   * The instance *is* the conversation, so sharing one shares what was said. The
   * token is returned only by `create` — the controller stores its digest — so a
   * caller that discards it cannot show it again.
   */
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
    input: { namespace: string; shareId: string };
    output: void;
  };

  "agentInstances.suspend": { input: AgentInstanceRef; output: AgentInstance };
  "agentInstances.resume": { input: AgentInstanceRef; output: AgentInstance };

  /**
   * The harnesses in one namespace, or in every observed namespace when omitted.
   *
   * `HarnessService`, not `AgentService` — `Harness` and `AgentHarness` are
   * different CRDs and only the names collide. See `domain/harnesses`.
   */
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
