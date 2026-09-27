import { describe, expect, it } from "vitest";
import { harnessDraftFromSpec, harnessDraftProblems, harnessSpecFromDraft } from "./harnessDraft";
import type { HarnessSpec } from "@/api/domain/harnesses";

const image = `ghcr.io/example/runtime@sha256:${"a".repeat(64)}`;
const spec: HarnessSpec = {
  kagent: { memory: { modelConfigRef: { name: "m" } } },
  workload: { image },
  substrate: { workerPoolRef: { name: "pool" }, snapshotPolicy: { location: "s3://snap" } },
  env: [{ name: "A", value: "1" }],
};

describe("harness draft", () => {
  it("round-trips a spec and keeps fields the form does not author", () => {
    const draft = harnessDraftFromSpec(spec);
    expect(harnessDraftProblems(draft)).toEqual([]);
    expect(harnessSpecFromDraft(draft, spec)).toEqual(spec);
  });
  it("switching adapter drops the old one so exactly one remains", () => {
    const next = harnessSpecFromDraft({ ...harnessDraftFromSpec(spec), adapter: "codex" }, spec);
    expect(next.kagent).toBeUndefined();
    expect(next.codex).toEqual({});
  });
  it("requires a digest pin and a command for byo", () => {
    const draft = { ...harnessDraftFromSpec(spec), image: "x:latest", adapter: "byo" as const };
    expect(harnessDraftProblems(draft)).toHaveLength(2);
  });
});
