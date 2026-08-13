import {
  buildSandboxSubstrateFromForm,
  isSubstrateSandboxAgent,
  sandboxChatMode,
  sandboxFieldsFromApiSpec,
  substrateSupportedForAgentType,
} from "@/lib/sandboxAgentForm";
import type { AgentFormData } from "@/lib/agentFormDomain";
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

  it("builds substrate config from form fields", () => {
    expect(
      buildSandboxSubstrateFromForm({
        ...base,
        substrateWorkerPoolRefName: " wp ",
        substrateSnapshotsLocation: " gs://snap ",
      }),
    ).toEqual({
      workerPoolRef: { name: "wp" },
      snapshotsConfig: { location: "gs://snap" },
    });
  });

  it("includes empty substrate object when optional fields are unset", () => {
    expect(buildSandboxSubstrateFromForm(base)).toEqual({});
  });
});

describe("substrate sandbox chat helpers", () => {
  const substrateSandbox = {
    agent: { kind: "SandboxAgent", spec: {} },
  } as AgentResponse;

  it("detects sandbox agents as substrate agents", () => {
    expect(isSubstrateSandboxAgent(substrateSandbox)).toBe(true);
  });

  it("maps sandbox chat mode", () => {
    expect(sandboxChatMode(substrateSandbox)).toBe("multi-session");
    expect(sandboxChatMode(undefined)).toBe("default");
  });
});

describe("substrateSupportedForAgentType", () => {
  it("allows substrate for declarative and BYO agents", () => {
    expect(substrateSupportedForAgentType("Declarative")).toBe(true);
    expect(substrateSupportedForAgentType("BYO")).toBe(true);
    expect(substrateSupportedForAgentType(undefined)).toBe(true);
  });
});
