import {
  buildSandboxSubstrateFromForm,
  isSubstrateSandboxAgent,
  sandboxChatMode,
  sandboxFieldsFromApiSpec,
  substrateSupportedForAgentType,
} from "@/lib/sandboxAgentForm";
import type { AgentFormData } from "@/components/AgentsProvider";
import type { AgentResponse } from "@/types";

describe("sandboxFieldsFromApiSpec", () => {
  it("maps substrate sandbox spec to form fields", () => {
    expect(
      sandboxFieldsFromApiSpec({
        workerPoolRef: { name: "pool-a" },
        snapshotsConfig: { location: "gs://bucket/snapshots" },
      }),
    ).toEqual({
      substrateWorkerPoolRefName: "pool-a",
      substrateSnapshotsLocation: "gs://bucket/snapshots",
    });
  });

  it("defaults to empty fields when substrate spec is unset", () => {
    expect(sandboxFieldsFromApiSpec(undefined)).toEqual({
      substrateWorkerPoolRefName: "",
      substrateSnapshotsLocation: "",
    });
  });
});

describe("buildSandboxSubstrateFromForm", () => {
  const base: AgentFormData = {
    name: "demo",
    namespace: "default",
    description: "d",
    tools: [],
  };

  it("omits sandbox config when not running in a sandbox", () => {
    expect(buildSandboxSubstrateFromForm({ ...base, runInSandbox: false })).toBeUndefined();
  });

  it("builds substrate config from form fields", () => {
    expect(
      buildSandboxSubstrateFromForm({
        ...base,
        runInSandbox: true,
        substrateWorkerPoolRefName: " wp ",
        substrateSnapshotsLocation: " gs://snap ",
      }),
    ).toEqual({
      workerPoolRef: { name: "wp" },
      snapshotsConfig: { location: "gs://snap" },
    });
  });

  it("includes empty substrate object when optional fields are unset", () => {
    expect(buildSandboxSubstrateFromForm({ ...base, runInSandbox: true })).toEqual({});
  });
});

describe("substrate sandbox chat helpers", () => {
  const substrateSandbox = {
    workloadMode: "sandbox",
    agent: { spec: {} },
  } as AgentResponse;

  const deployment = {
    workloadMode: "deployment",
    agent: { spec: {} },
  } as AgentResponse;

  it("detects sandbox agents as substrate agents", () => {
    expect(isSubstrateSandboxAgent(substrateSandbox)).toBe(true);
    expect(isSubstrateSandboxAgent(deployment)).toBe(false);
  });

  it("maps sandbox chat mode", () => {
    expect(sandboxChatMode(substrateSandbox)).toBe("multi-session");
    expect(sandboxChatMode(deployment)).toBe("default");
  });
});

describe("substrateSupportedForAgentType", () => {
  it("disallows substrate for BYO agents", () => {
    expect(substrateSupportedForAgentType("BYO")).toBe(false);
  });
  it("allows substrate for declarative agents", () => {
    expect(substrateSupportedForAgentType("Declarative")).toBe(true);
    expect(substrateSupportedForAgentType(undefined)).toBe(true);
  });
});
