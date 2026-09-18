/**
 * A `Harness`: the runtime half of what an agent is made of.
 *
 * An `AgentInstance` is created from a pair — a `Harness` and an `AgentTemplate`
 * — and this is the half that says *how* the agent runs: which runtime adapter
 * (`kagent`, `codex` or `claude`), which Substrate worker pool, which snapshot
 * policy, and a digest-pinned workload image. The template is the other half and
 * says what the agent *is*: its model, its prompt, its tools.
 *
 * A Harness is reusable and admits many templates through a label selector.
 * `HarnessService` owns its API.
 */

import type { ResourceMetadata } from "./common";

/** The runtime adapters a harness can select. */
export type HarnessRuntime = "kagent" | "codex" | "claude";

export interface Harness {
  /** `namespace/name`. */
  ref: string;
  namespace: string;
  name: string;

  /**
   * The adapter the spec selects.
   *
   * Denormalised by the controller because the CRD enforces an exactly-one-of
   * across three spec fields, and every caller listing harnesses would otherwise
   * reimplement that check. A value outside the three is passed through as it
   * arrived rather than being folded into a plausible one.
   */
  runtime: string;

  /** `spec.workload.image`. Digest-pinned — a tag is rejected by CEL on the CRD. */
  workloadImage: string;

  /**
   * The `Ready` status condition.
   *
   * False also covers a harness the controller has not observed yet, which is not
   * the same thing as one that failed — so a page saying "not ready" is right and
   * a page saying "broken" would not be.
   */
  ready: boolean;

  /**
   * The whole custom resource, for anything the fields above do not carry.
   *
   * The spec is carried verbatim rather than re-modelled, exactly as the service
   * carries it: a CRD gaining a field cannot then drift silently from a partial
   * copy of it here. `unknown` because nothing in this app reads into it yet, and
   * a shape written speculatively is the thing that drifts.
   */
  resource: { metadata: ResourceMetadata; spec?: unknown; status?: unknown };
}

/**
 * A harness as it is written, for creating one.
 *
 * The spec is modelled here rather than left `unknown` — unlike the one carried on a
 * read, which is deliberately opaque so a CRD gaining a field cannot drift from a
 * partial copy. Writing needs the opposite: a form has to know what it is allowed to
 * send, and the CRD's own constraints are what the form validates against.
 */
export interface HarnessResource {
  metadata: ResourceMetadata;
  spec: HarnessSpec;
}

/**
 * What a harness is, in the shape the CRD accepts.
 *
 * Three constraints are enforced by CEL on the resource and so are worth stating where
 * a form can see them:
 *
 * - exactly one of `kagent`, `codex` or `claude` picks the runtime adapter;
 * - `workload.image` must be pinned by digest — a tag is rejected outright;
 * - `substrate.workerPoolRef.name` must not be empty.
 *
 * `allowedAgentTemplates` is optional and omitting it admits *no* templates, which is
 * a harness that runs nothing. That is legal and almost never intended.
 */
export interface HarnessSpec {
  kagent?: Record<string, never>;
  codex?: Record<string, never>;
  claude?: Record<string, never>;
  workload: { image: string };
  substrate: {
    workerPoolRef: { name: string };
    snapshotPolicy: { location: string };
  };
  allowedAgentTemplates?: { selector: { matchLabels?: Record<string, string> } };
  env?: { name: string; value?: string }[];
}

/** The adapters a harness may select, exactly one of which is required. */
export const HARNESS_ADAPTERS = ["kagent", "codex", "claude"] as const;
export type HarnessAdapter = (typeof HARNESS_ADAPTERS)[number];

/** The digest pin `workload.image` must satisfy, from the CRD's own pattern. */
export const HARNESS_IMAGE_PATTERN = /^[^\s@]+@sha256:[a-f0-9]{64}$/;

/**
 * The `matchLabels` a harness admits templates on.
 *
 * Lives here rather than beside either caller because three places need it — the
 * template form's preview, the fixture backend's admission calculation, and the
 * agent-create page's "label it for this harness" button — and three copies of a
 * selector walk is three chances to disagree about what admission means.
 *
 * Only `matchLabels` is read. A `LabelSelector` can also carry `matchExpressions`,
 * and this deliberately does not attempt them: `In` would need a value chosen for
 * the reader, and `DoesNotExist` is satisfied by doing nothing at all. A harness
 * using them reports no selector here, so the form offers no button for it rather
 * than offering one that would produce a template it still would not admit.
 */
export function harnessSelector(harness: Harness): Record<string, string> {
  const spec = harness.resource.spec as
    | { allowedAgentTemplates?: { selector?: { matchLabels?: Record<string, string> } } }
    | undefined;
  return spec?.allowedAgentTemplates?.selector?.matchLabels ?? {};
}

/**
 * Whether a set of labels is admitted by a harness.
 *
 * A harness with no selector admits none — the CRD says so — which is why an empty
 * selector is `false` here rather than "matches everything".
 */
export function admitsLabels(
  harness: Harness,
  labels: Record<string, string>,
): boolean {
  const selector = harnessSelector(harness);
  const keys = Object.keys(selector);
  if (keys.length === 0) return false;
  return keys.every((key) => labels[key] === selector[key]);
}
