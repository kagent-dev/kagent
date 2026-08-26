/**
 * An **agent**: one `AgentTemplate` paired with one `Harness`.
 *
 * ## Why this is the agent, and an `AgentInstance` is not
 *
 * An instance is a *conversation*. Upstream's own end-to-end test asserts that a
 * task's `ContextID` **is** the instance id (`go/core/test/e2e/interaction_test.go`),
 * `agent_instance_task` holds the turns within one instance, and there is no way to
 * open a second context under one instance. So an instance is one thread of talk,
 * created and thrown away as freely as a chat window.
 *
 * What persists is the pair. `agent_template_harness_pair` is a real table keyed by
 * `(namespace, agent_template_uid, harness_uid)` carrying `desired_revision`,
 * `latest_successful_revision` and `retired_at` — a lifecycle of its own, which the
 * controller materialises from admission and retires when the labels stop matching.
 * That is the durable, runnable thing you can talk to more than once.
 *
 * ## Listing agents costs nothing
 *
 * The pair is already on the wire: `AgentTemplate.status.harnesses[]` has one entry
 * per admitting harness, which is one entry per pair. `ListAgentTemplates` therefore
 * carries the whole agent list, its revision state included — there is no pair
 * service to call and none is needed.
 *
 * ## An agent is named by its template
 *
 * A pair is *derived* rather than authored: nobody creates one, so there is nothing
 * to hang a name on and no RPC that could store it. In practice a template is
 * admitted by exactly one harness and the agent's name is the template's. Where a
 * template is admitted by two, that pair is two agents sharing a name and told apart
 * by their harness — which is why the harness is a column rather than a detail.
 */

import type {
  AgentTemplate,
  AgentTemplateCondition,
  AgentTemplateHarnessStatus,
} from "./agentTemplates";

/**
 * Whether the controller has built something this pair can actually run.
 *
 * Three states rather than a boolean, because "not ready" hides a distinction that
 * matters to whoever is looking at the row:
 *
 * - `ready` — a `latestSuccessfulRevision` exists, so `CreateAgentInstance` will be
 *   accepted for this pair.
 * - `preparing` — a revision is desired and none has succeeded yet. Ordinary, and
 *   what a freshly-labelled template looks like for its first few seconds.
 * - `notReported` — the controller has said nothing about this pair at all. Not the
 *   same as a failure, and saying "broken" about it would be inventing a fact.
 */
export type AgentRevisionState = "ready" | "preparing" | "notReported";

/** One agent: a template, a harness, and what the controller made of the two. */
export interface AgentPair {
  /**
   * `namespace/template/harness`, unique across the cluster.
   *
   * Three parts because two are not enough: a template admitted by two harnesses is
   * two agents, and a key that omitted the harness would collapse them into one row.
   */
  id: string;
  namespace: string;
  /** The template's name, which is what the agent is called. */
  agentTemplate: string;
  /** The harness's name, within the same namespace — admission never crosses one. */
  harness: string;
  /** The template's own description, or empty where it has none. */
  description: string;
  revisionState: AgentRevisionState;
  /** The revision an instance would currently be cut from, when there is one. */
  latestSuccessfulRevision?: string;
  /**
   * Why the pair is not ready, in the controller's own words, when it said.
   *
   * Taken from the conditions on this harness's status entry rather than composed
   * here: a reason invented by the client is a reason nobody can look up.
   */
  notReadyReason?: string;
  /**
   * The template this pair was assembled from, for the surfaces that need its spec.
   *
   * Absent on exactly one row: the synthetic agent that gathers conversations whose
   * pair no longer exists. That row is not assembled from a template — the template is
   * the thing that went — so anything reading this must handle its absence rather than
   * assume every row on the agents page came from one.
   */
  template?: AgentTemplate;
  /**
   * True for the synthetic row that holds conversations belonging to no agent.
   *
   * A marker rather than a name check: the row is addressed by a reserved id, and a
   * page comparing display names would break the moment somebody made a template
   * called the same thing.
   */
  isUnmapped?: boolean;
}

/** The `status.harnesses[]` entries, whatever shape the resource arrived in. */
function harnessStatuses(template: AgentTemplate): AgentTemplateHarnessStatus[] {
  return template.resource.status?.harnesses ?? [];
}

/**
 * The condition that explains a pair, or `undefined`.
 *
 * The CRD caps conditions at four and does not fix which are present, so this looks
 * for any condition the controller has marked false rather than for one named type —
 * a hard-coded `"Ready"` would silently report nothing on a controller that names
 * its condition something else.
 */
function firstFalseCondition(
  conditions: AgentTemplateCondition[] | undefined,
): AgentTemplateCondition | undefined {
  return (conditions ?? []).find((condition) => condition.status !== "True");
}

function revisionStateOf(status: AgentTemplateHarnessStatus): AgentRevisionState {
  if (status.latestSuccessfulRevision) return "ready";
  if (status.desiredRevision) return "preparing";
  return "notReported";
}

/**
 * The agents one template is, one per admitting harness.
 *
 * Read from `status.harnesses[]` rather than from `admittingHarnesses`, even though
 * the controller derives the latter from the former: the status entries carry the
 * revision state as well as the name, and taking the names from one field and the
 * revisions from another is two lookups that can disagree.
 *
 * A template nothing admits yields no agents, which is the truth about it — with no
 * admitting harness there is no prepared revision and every `CreateAgentInstance`
 * naming it is refused. It is still a template, and the templates page is where it
 * is listed and where the admission warning belongs.
 */
export function agentPairsOf(template: AgentTemplate): AgentPair[] {
  return harnessStatuses(template).map((status) => {
    const failing = firstFalseCondition(status.conditions);
    return {
      id: `${template.namespace}/${template.name}/${status.harness}`,
      namespace: template.namespace,
      agentTemplate: template.name,
      harness: status.harness,
      description: template.description,
      revisionState: revisionStateOf(status),
      latestSuccessfulRevision: status.latestSuccessfulRevision,
      notReadyReason: failing
        ? (failing.message ?? failing.reason ?? undefined)
        : undefined,
      template,
    };
  });
}

/**
 * Every agent across a set of templates, in a stable order.
 *
 * Sorted by namespace, then template, then harness — so the two agents one template
 * is sit next to each other and the difference between them is the column that
 * changes. The order is fixed here rather than left to the read so that a refetch
 * cannot rearrange rows under a reader's cursor.
 */
export function agentPairsFrom(templates: readonly AgentTemplate[]): AgentPair[] {
  return templates
    .flatMap(agentPairsOf)
    .sort(
      (a, b) =>
        a.namespace.localeCompare(b.namespace) ||
        a.agentTemplate.localeCompare(b.agentTemplate) ||
        a.harness.localeCompare(b.harness),
    );
}

/**
 * The pair one conversation belongs to, as an id comparable with `AgentPair.id`.
 *
 * An instance reports its harness and template as `namespace/name` refs, so both are
 * reduced to bare names here. `undefined` when the record names neither — an
 * instance with no prepared revision belongs to no pair, which is exactly what the
 * controller's own `LEFT JOIN` says about it.
 */
export function pairIdOfInstance(instance: {
  namespace: string;
  harness?: string;
  agentTemplate?: string;
}): string | undefined {
  if (!instance.harness || !instance.agentTemplate) return undefined;
  return `${instance.namespace}/${bareName(instance.agentTemplate)}/${bareName(instance.harness)}`;
}

/**
 * The name half of a `namespace/name` ref.
 *
 * `AgentInstance` reports qualified refs while every RPC that takes one of these
 * takes a bare name — a pair is always in the instance's own namespace, so the
 * qualification carries nothing the request needs and a qualified value is refused
 * as an invalid name.
 */
export function bareName(ref: string): string {
  const slash = ref.lastIndexOf("/");
  return slash === -1 ? ref : ref.slice(slash + 1);
}

/**
 * Why this agent cannot start a conversation right now, in words, or `undefined`.
 *
 * The controller's precondition rather than a preference: `CreateAgentInstance`
 * answers `FailedPrecondition` — *"AgentTemplate and Harness do not have a ready
 * prepared revision"* — for a pair with no successful revision. Asking first means a
 * disabled button that explains itself instead of a button that produces a refusal.
 */
export function newConversationBlockedReason(pair: AgentPair): string | undefined {
  if (pair.revisionState === "ready") return undefined;
  if (pair.revisionState === "preparing") {
    return (
      pair.notReadyReason ??
      "The controller is still preparing a revision for this agent. Conversations can start once one has succeeded."
    );
  }
  return "The controller has not reported a revision for this agent yet, so there is nothing to start a conversation from.";
}

/** The reserved id and name of the row that gathers conversations with no agent. */
export const UNMAPPED_AGENT_ID = "__unmapped__";
export const UNMAPPED_AGENT_NAME = "unmapped-agentinstances";

/**
 * A stand-in agent for conversations whose pair no longer exists.
 *
 * Deleting a template does not stop the conversations cut from it — an instance runs
 * from the prepared revision it was built against, and the collector keeps that
 * revision for it. So a conversation can outlive its agent, and until now those
 * conversations were counted in a sentence and reachable from nowhere.
 *
 * This gives them somewhere to be. It is deliberately not a real pair: it has no
 * template, because the template is the thing that went, and `isUnmapped` says so to
 * anything that would otherwise treat it as one.
 */
export function unmappedAgent(namespace: string): AgentPair {
  return {
    id: UNMAPPED_AGENT_ID,
    namespace,
    agentTemplate: UNMAPPED_AGENT_NAME,
    harness: "—",
    description:
      "Conversations whose template and harness no longer pair. They still run, and can be read and deleted here.",
    revisionState: "notReported",
    isUnmapped: true,
  };
}
