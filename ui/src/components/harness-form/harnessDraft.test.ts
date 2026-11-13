import { describe, expect, it } from "vitest";
import { emptyHarnessDraft, harnessDraftFromSpec, harnessDraftProblems, harnessSpecFromDraft } from "./harnessDraft";
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
  it("keeps toolImages the form does not author", () => {
    const toolImages = [{ name: "kubectl", oci: `ghcr.io/example/kubectl@sha256:${"b".repeat(64)}` }];
    const withTools: HarnessSpec = { ...spec, workload: { image, toolImages } };
    const next = harnessSpecFromDraft(harnessDraftFromSpec(withTools), withTools);
    expect(next.workload).toEqual({ image, toolImages });
    const releaseImage = harnessSpecFromDraft({ ...harnessDraftFromSpec(withTools), image: "" }, withTools);
    expect(releaseImage.workload).toEqual({ toolImages });
    expect(harnessSpecFromDraft(harnessDraftFromSpec(spec), spec).workload).toEqual({ image });
  });
  it("switching adapter drops the old one so exactly one remains", () => {
    const next = harnessSpecFromDraft({ ...harnessDraftFromSpec(spec), adapter: "codex" }, spec);
    expect(next.kagent).toBeUndefined();
    expect(next.codex).toEqual({});
  });
  it("drops command and args for adapters that do not support them", () => {
    const draft = { ...harnessDraftFromSpec(spec), command: ["/bin/sh"], args: ["-c"] };
    for (const adapter of ["claude", "codex"] as const) {
      expect(harnessSpecFromDraft({ ...draft, adapter }, spec).workload).toEqual({ image });
    }
    expect(harnessSpecFromDraft(draft, spec).workload).toEqual({ image, command: ["/bin/sh"], args: ["-c"] });
  });
  it("starts on the release image and the default snapshot location", () => {
    const draft = { ...emptyHarnessDraft(), workerPool: "kagent-default" };
    expect(harnessDraftProblems(draft)).toEqual([]);
    expect(harnessSpecFromDraft(draft)).toEqual({
      kagent: {},
      substrate: {
        workerPoolRef: { name: "kagent-default" },
        snapshotPolicy: { location: "s3://ate-snapshots/kagent/" },
      },
    });
  });
  it("clearing the image omits the workload so the release image applies", () => {
    const next = harnessSpecFromDraft({ ...harnessDraftFromSpec(spec), image: " " }, spec);
    expect(next.workload).toBeUndefined();
    expect(next.env).toEqual(spec.env);
  });
  it("keeps a command when the image is cleared", () => {
    const next = harnessSpecFromDraft({ ...harnessDraftFromSpec(spec), image: "", command: ["run"] }, spec);
    expect(next.workload).toEqual({ command: ["run"] });
  });
  it("rejects a tag but not an empty image on a built-in adapter", () => {
    expect(harnessDraftProblems({ ...harnessDraftFromSpec(spec), image: "x:latest" })).toEqual([
      "The workload image must be pinned by sha256 digest.",
    ]);
    expect(harnessDraftProblems({ ...harnessDraftFromSpec(spec), image: "" })).toEqual([]);
  });
  it("requires an image and a command for byo", () => {
    const draft = { ...harnessDraftFromSpec(spec), image: "", adapter: "byo" as const };
    expect(harnessDraftProblems(draft)).toEqual([
      "A bring-your-own harness needs a workload image.",
      "A bring-your-own harness needs a command.",
    ]);
  });
});
