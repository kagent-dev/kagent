import {
  HARNESS_ADAPTERS,
  HARNESS_IMAGE_PATTERN,
  type HarnessAdapter,
  type HarnessSpec,
} from "@/api/domain/harnesses";

export interface HarnessDraft {
  adapter: HarnessAdapter;
  image: string;
  command: string[];
  args: string[];
  workerPool: string;
  snapshotLocation: string;
}

export function emptyHarnessDraft(): HarnessDraft {
  return { adapter: "kagent", image: "", command: [], args: [], workerPool: "", snapshotLocation: "" };
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

/** Merged onto `existing` so fields this form does not author (env, kagent policy) survive an edit. */
export function harnessSpecFromDraft(draft: HarnessDraft, existing?: HarnessSpec): HarnessSpec {
  const spec: HarnessSpec = { ...existing } as HarnessSpec;
  for (const adapter of HARNESS_ADAPTERS) {
    if (adapter !== draft.adapter) delete spec[adapter];
  }
  return {
    ...spec,
    // Exactly one adapter, which the CRD requires.
    [draft.adapter]: existing?.[draft.adapter] ?? {},
    workload: {
      image: draft.image.trim(),
      ...(draft.command.length > 0 ? { command: draft.command } : {}),
      ...(draft.args.length > 0 ? { args: draft.args } : {}),
    },
    substrate: {
      ...existing?.substrate,
      workerPoolRef: { name: draft.workerPool.trim() },
      snapshotPolicy: { location: draft.snapshotLocation.trim() },
    },
  };
}

/** Only rules the CRD enforces, so the form never blocks a harness the cluster would accept. */
export function harnessDraftProblems(draft: HarnessDraft): string[] {
  const problems: string[] = [];
  if (!HARNESS_IMAGE_PATTERN.test(draft.image.trim())) problems.push("A digest-pinned workload image is required.");
  if (draft.adapter === "byo" && draft.command.length === 0) problems.push("A bring-your-own harness needs a command.");
  if (draft.workerPool.trim() === "") problems.push("A worker pool is required.");
  if (draft.snapshotLocation.trim() === "") problems.push("A snapshot location is required.");
  return problems;
}
