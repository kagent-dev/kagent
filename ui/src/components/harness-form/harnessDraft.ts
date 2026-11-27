import {
  DEFAULT_HARNESS_SNAPSHOT_LOCATION,
  HARNESS_ADAPTERS,
  HARNESS_IMAGE_PATTERN,
  type HarnessAdapter,
  type HarnessSpec,
} from "@/api/domain/harnesses";
import type { FieldIssue } from "@/components/form/useSubmitValidation";

export interface HarnessDraft {
  adapter: HarnessAdapter;
  image: string;
  command: string[];
  args: string[];
  workerPool: string;
  snapshotLocation: string;
}

/** What the adapter dropdown shows; the stored value stays the CRD's key. */
export const HARNESS_ADAPTER_LABELS: Record<HarnessAdapter, string> = {
  kagent: "kagent",
  codex: "Codex",
  claude: "Claude",
  byo: "Bring your own image",
};

/** Whether the adapter honors workload command and args overrides. The CRD rejects them for the rest. */
export function adapterSupportsCommand(adapter: HarnessAdapter): boolean {
  return adapter === "kagent" || adapter === "byo";
}

/**
 * A new harness on the release's built-in image. The worker pool stays empty until the
 * form knows which pools exist, so it never names one the cluster does not have.
 */
export function emptyHarnessDraft(): HarnessDraft {
  return {
    adapter: "kagent",
    image: "",
    command: [],
    args: [],
    workerPool: "",
    snapshotLocation: DEFAULT_HARNESS_SNAPSHOT_LOCATION,
  };
}

export function harnessDraftFromSpec(spec: HarnessSpec): HarnessDraft {
  return {
    adapter: HARNESS_ADAPTERS.find((adapter) => spec[adapter] !== undefined) ?? "kagent",
    image: spec.workload?.image ?? "",
    command: [...(spec.workload?.command ?? [])],
    args: [...(spec.workload?.args ?? [])],
    workerPool: spec.substrate?.workerPoolRef?.name ?? "",
    snapshotLocation: spec.substrate?.snapshotPolicy?.location ?? "",
  };
}

/**
 * Merged onto `existing` so fields this form does not author (env, kagent policy) survive an edit.
 * An empty image is omitted, and so is an empty workload, so the harness runs the release's image.
 */
export function harnessSpecFromDraft(draft: HarnessDraft, existing?: HarnessSpec): HarnessSpec {
  const spec: HarnessSpec = { ...existing } as HarnessSpec;
  const overrides = adapterSupportsCommand(draft.adapter);
  for (const adapter of HARNESS_ADAPTERS) {
    if (adapter !== draft.adapter) delete spec[adapter];
  }
  delete spec.workload;
  const image = draft.image.trim();
  const workload = {
    ...(image ? { image } : {}),
    ...(overrides && draft.command.length > 0 ? { command: draft.command } : {}),
    ...(overrides && draft.args.length > 0 ? { args: draft.args } : {}),
  };
  return {
    ...spec,
    // Exactly one adapter, which the CRD requires.
    [draft.adapter]: existing?.[draft.adapter] ?? {},
    ...(Object.keys(workload).length > 0 ? { workload } : {}),
    substrate: {
      ...existing?.substrate,
      workerPoolRef: { name: draft.workerPool.trim() },
      snapshotPolicy: { location: draft.snapshotLocation.trim() },
    },
  };
}

export type HarnessDraftField = "image" | "command" | "workerPool" | "snapshotLocation";

/** Only rules the CRD enforces, so the form never blocks a harness the cluster would accept. */
export function harnessDraftIssues(draft: HarnessDraft): FieldIssue<HarnessDraftField>[] {
  const issues: FieldIssue<HarnessDraftField>[] = [];
  const image = draft.image.trim();
  if (image === "" && draft.adapter === "byo") {
    issues.push({ field: "image", message: "A bring-your-own harness needs a workload image." });
  }
  if (image !== "" && !HARNESS_IMAGE_PATTERN.test(image)) {
    issues.push({ field: "image", message: "The workload image must be pinned by sha256 digest." });
  }
  if (draft.adapter === "byo" && draft.command.length === 0) {
    issues.push({ field: "command", message: "A bring-your-own harness needs a command." });
  }
  if (draft.workerPool.trim() === "") issues.push({ field: "workerPool", message: "A worker pool is required." });
  if (draft.snapshotLocation.trim() === "") {
    issues.push({ field: "snapshotLocation", message: "A snapshot location is required." });
  }
  return issues;
}
