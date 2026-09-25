

import type { ResourceMetadata } from "./common";

/** The runtime adapters a harness can select. */
export type HarnessRuntime = "kagent" | "codex" | "claude" | "byo";

export interface Harness {
  /** `namespace/name`. */
  ref: string;
  namespace: string;
  name: string;

  /**
   * The adapter the spec selects.
   *
   * Denormalised by the controller because the CRD enforces an exactly-one-of
   * across four spec fields, and every caller listing harnesses would otherwise
   * reimplement that check. A value outside the four is passed through as it
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


/** `spec.kagent.compaction.summarizer`: the model and prompt that write the summaries. */
export interface KagentHarnessSummarizer {
  /** A ModelConfig in the harness namespace. Omitted summarizes with the agent's own model. */
  modelConfigRef?: { name: string };
  /** Must contain `{conversation_history}`. */
  promptTemplate?: string;
}

/**
 * `spec.kagent.compaction`: the sliding window (`compactionInterval`, `overlapSize`)
 * and tail retention (`tokenThreshold`, `eventRetentionSize`) strategies. At least
 * one strategy is required; `tokenThreshold` and `eventRetentionSize` go together.
 */
export interface KagentHarnessCompaction {
  compactionInterval?: number;
  overlapSize?: number;
  tokenThreshold?: number;
  eventRetentionSize?: number;
  summarizer?: KagentHarnessSummarizer;
}

/** `spec.kagent`: runtime policy of the kagent adapter. */
export interface KagentHarnessSpec {
  memory?: { modelConfigRef: { name: string }; ttlDays?: number };
  compaction?: KagentHarnessCompaction;
}

export interface HarnessSpec {
  kagent?: KagentHarnessSpec;
  codex?: Record<string, never>;
  claude?: Record<string, never>;
  /** Bring your own: an image that serves kagent's A2A contract itself. */
  byo?: Record<string, never>;
  workload: { image: string; command?: string[]; args?: string[] };
  substrate: {
    workerPoolRef: { name: string };
    snapshotPolicy: { location: string };
  };
  env?: { name: string; value?: string }[];
}

/** The adapters a harness may select, exactly one of which is required. */
export const HARNESS_ADAPTERS = ["kagent", "codex", "claude", "byo"] as const;
export type HarnessAdapter = (typeof HARNESS_ADAPTERS)[number];

/** The digest pin `workload.image` must satisfy, from the CRD's own pattern. */
export const HARNESS_IMAGE_PATTERN = /^[^\s@]+@sha256:[a-f0-9]{64}$/;
