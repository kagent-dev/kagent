import type { Agent, AgentCreateRequest } from "@/api";

/**
 * An edit, applied on top of the agent it was read out of.
 *
 * ## Why an edit is not a create with the same name
 *
 * `agents.update` is a `PUT` of the whole resource, so whatever the request omits is
 * *removed* from the cluster. The form builds a complete agent from the fields it shows,
 * which is exactly right for a create and lossy for an edit: everything the form does not
 * model is absent from what it builds, and a save would delete it.
 *
 * What that meant in practice, before this existed: `executeCodeBlocks`,
 * `promptTemplate`, `systemMessageFrom` and everything under `declarative.deployment`
 * except the service account were deleted by any save. Losing them is invisible on the
 * page that caused it — the agent keeps working, with fewer replicas than it was given.
 *
 * ## What metadata is doing here
 *
 * Belt and braces, not a fix. The controller's update handler fetches the live object and
 * assigns `*existing.Spec = *incoming.Spec`, so the metadata in the request body is
 * ignored entirely — labels and annotations were never at risk from this app, and an
 * earlier version of this comment claimed they were. The merge still carries them,
 * because what the endpoint *is* is a PUT of a whole resource and a request that omits
 * them is wrong against that contract even where today's server forgives it.
 *
 * ## What is kept, and what the form is allowed to clear
 *
 * Metadata is carried through with the form's name and namespace on top. Two fields are
 * deliberately dropped: `creationTimestamp` and `resourceVersion` are the cluster's to
 * assign, and sending the ones that were read would claim to be writing a specific
 * version of the resource.
 *
 * In the spec, only keys the form actually owns are taken from the edit; the rest are
 * carried through. That distinction is the whole subtlety here. A blanket "prefer the
 * edit" would clear a field the form does not know about, and a blanket "prefer what was
 * there" would ignore the reader clearing a field they can see — a form that owns a key
 * omits it precisely to mean "no longer set", which is how removing every skill removes
 * `a2aConfig`.
 */
/**
 * What the form owns, level by level.
 *
 * Ownership has to be stated per level, not per block, because the form edits a *few*
 * fields of a nested object and has never heard of its siblings. `declarative` is the
 * case that matters: the form models nine of its thirteen fields, so treating the block
 * as owned deleted `promptTemplate`, `executeCodeBlocks` and `systemMessageFrom` from any
 * agent that had them. `deployment` is worse — the form sets `serviceAccountName` and the
 * CRD has eighteen other fields there, including `replicas`, `env`, `resources`,
 * `volumes` and `tolerations`.
 *
 * `substrate` is listed and never arrives: the Agent CRD has no such field — it belongs
 * to `SandboxAgent` — so the API server prunes what the form writes there. Listing it is
 * honest about the form's intent; the form offering the fields at all is the actual bug,
 * and it is not this function's to fix.
 */
interface Ownership {
  /** Keys the form decides. One it omits means "no longer set". */
  keys: readonly string[];
  /** Keys whose *contents* are owned only in part. */
  nested?: Record<string, Ownership>;
}

const DEPLOYMENT: Ownership = { keys: ["serviceAccountName"] };

const DECLARATIVE: Ownership = {
  keys: [
    "modelConfig",
    "systemMessage",
    "tools",
    "runtime",
    "stream",
    "shareTools",
    "a2aConfig",
    "memory",
    "context",
    "deployment",
  ],
  nested: { deployment: DEPLOYMENT },
};

const SPEC: Ownership = {
  keys: ["type", "description", "declarative", "byo", "substrate"],
  nested: { declarative: DECLARATIVE, byo: { keys: ["deployment"] } },
};

/**
 * The same ownership, for a save that changes the agent's *kind*.
 *
 * Preserving what the form cannot see is the right instinct everywhere except here. A
 * declarative agent becoming a BYO one should not keep a `declarative` block, however
 * many fields in it the form never modelled — the block belongs to the kind it is no
 * longer. So on a kind change both blocks are wholly owned: the edit supplies the new
 * one and the old one goes.
 */
const SPEC_ACROSS_KINDS: Ownership = { keys: SPEC.keys };

/**
 * The edit's values for the keys it owns, on top of everything it does not.
 *
 * Clearing works by construction at each level: an owned key the edit omits is simply not
 * written. A *partly* owned block is different — the edit omitting `deployment` means "no
 * service account", not "delete the replicas too" — so a nested block is always merged,
 * and dropped only when the merge leaves it empty.
 */
function mergeOwned(
  existing: Record<string, unknown> | undefined,
  edited: Record<string, unknown> | undefined,
  ownership: Ownership,
): Record<string, unknown> | undefined {
  const owned = new Set(ownership.keys);
  const merged: Record<string, unknown> = Object.fromEntries(
    Object.entries(existing ?? {}).filter(([key]) => !owned.has(key)),
  );

  for (const key of ownership.keys) {
    const value = edited?.[key];
    const nested = ownership.nested?.[key];

    if (nested) {
      const inner = mergeOwned(
        existing?.[key] as Record<string, unknown> | undefined,
        value as Record<string, unknown> | undefined,
        nested,
      );
      // An empty block is noise on the resource, and `{}` is not what "unset" looks like.
      if (inner && Object.keys(inner).length > 0) merged[key] = inner;
      continue;
    }

    if (value !== undefined) merged[key] = value;
  }

  return merged;
}

export function agentUpdatePayload(
  existing: Agent,
  edited: AgentCreateRequest,
): AgentCreateRequest {
  const changesKind = edited.spec.type !== existing.spec.type;

  const spec = mergeOwned(
    existing.spec as unknown as Record<string, unknown>,
    edited.spec as unknown as Record<string, unknown>,
    changesKind ? SPEC_ACROSS_KINDS : SPEC,
  );

  return {
    // The API version and kind the cluster gave us, so an agent stored under an older
    // version is not silently rewritten as a newer one by editing its description.
    apiVersion: edited.apiVersion ?? existing.apiVersion,
    kind: edited.kind ?? existing.kind,
    metadata: {
      ...existing.metadata,
      ...edited.metadata,
      creationTimestamp: undefined,
      resourceVersion: undefined,
    },
    spec: spec as unknown as AgentCreateRequest["spec"],
  };
}
