import {
  buildSandboxPlatformFromForm,
  buildSandboxSubstrateFromForm,
  defaultDeclarativeRuntimeForSandboxPlatform,
  defaultSandboxPlatform,
  isSingleSessionSandboxAgent,
  isSubstrateSandboxAgent,
  sandboxChatMode,
  sandboxFieldsFromApiSpec,
  skillsSupportedForSandboxPlatform,
  substrateSupportedForAgentType,
} from "@/lib/sandboxAgentForm";
import type { AgentFormData } from "@/components/AgentsProvider";
import type { AgentResponse } from "@/types";

describe("sandboxFieldsFromApiSpec", () => {
  it("maps substrate sandbox spec to form fields", () => {
    expect(
      sandboxFieldsFromApiSpec("substrate", {
        workerPoolRef: { name: "pool-a" },
        snapshotsConfig: { location: "gs://bucket/snapshots" },
      }),
    ).toEqual({
      sandboxPlatform: "substrate",
      substrateWorkerPoolRefName: "pool-a",
      substrateSnapshotsLocation: "gs://bucket/snapshots",
    });
  });

  it("defaults to agent-sandbox when platform is unset", () => {
    expect(sandboxFieldsFromApiSpec(undefined)).toEqual({
      sandboxPlatform: "agent-sandbox",
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

  it("omits sandbox when platform is agent-sandbox", () => {
    expect(buildSandboxSubstrateFromForm({ ...base, sandboxPlatform: "agent-sandbox" })).toBeUndefined();
  });

  it("builds substrate config from form fields", () => {
    expect(
      buildSandboxSubstrateFromForm({
        ...base,
        sandboxPlatform: "substrate",
        substrateWorkerPoolRefName: " wp ",
        substrateSnapshotsLocation: " gs://snap ",
      }),
    ).toEqual({
      workerPoolRef: { name: "wp" },
      snapshotsConfig: { location: "gs://snap" },
    });
  });

  it("includes empty substrate object when optional fields are unset", () => {
    expect(buildSandboxSubstrateFromForm({ ...base, sandboxPlatform: "substrate" })).toEqual({});
  });
});

describe("buildSandboxPlatformFromForm", () => {
  const base: AgentFormData = {
    name: "demo",
    namespace: "default",
    description: "d",
    tools: [],
  };

  it("emits substrate platform only when selected", () => {
    expect(buildSandboxPlatformFromForm({ ...base, sandboxPlatform: "substrate" })).toBe("substrate");
    expect(buildSandboxPlatformFromForm({ ...base, sandboxPlatform: "agent-sandbox" })).toBeUndefined();
  });
});

describe("defaultSandboxPlatform", () => {
  it("prefers substrate when enabled", () => {
    expect(defaultSandboxPlatform(true)).toBe("substrate");
  });

  it("falls back to agent-sandbox when substrate is unavailable", () => {
    expect(defaultSandboxPlatform(false)).toBe("agent-sandbox");
  });
});

describe("substrate sandbox chat helpers", () => {
  const substrateSandbox = {
    workloadMode: "sandbox",
    agent: { spec: { platform: "substrate" } },
  } as AgentResponse;

  const agentSandbox = {
    workloadMode: "sandbox",
    agent: { spec: { platform: "agent-sandbox" } },
  } as AgentResponse;

  const deployment = {
    workloadMode: "deployment",
    agent: { spec: {} },
  } as AgentResponse;

  it("detects substrate sandbox agents", () => {
    expect(isSubstrateSandboxAgent(substrateSandbox)).toBe(true);
    expect(isSubstrateSandboxAgent(agentSandbox)).toBe(false);
    expect(isSubstrateSandboxAgent(deployment)).toBe(false);
  });

  it("treats only classic sandbox agents as single-session", () => {
    expect(isSingleSessionSandboxAgent(substrateSandbox)).toBe(false);
    expect(isSingleSessionSandboxAgent(agentSandbox)).toBe(true);
    expect(isSingleSessionSandboxAgent(deployment)).toBe(false);
  });

  it("maps sandbox chat mode", () => {
    expect(sandboxChatMode(substrateSandbox)).toBe("multi-session");
    expect(sandboxChatMode(agentSandbox)).toBe("single-session");
    expect(sandboxChatMode(deployment)).toBe("default");
  });
});

describe("defaultDeclarativeRuntimeForSandboxPlatform", () => {
  it("defaults substrate sandbox agents to Go runtime", () => {
    expect(defaultDeclarativeRuntimeForSandboxPlatform("substrate")).toBe("go");
    expect(defaultDeclarativeRuntimeForSandboxPlatform("agent-sandbox")).toBe("python");
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

describe("skillsSupportedForSandboxPlatform", () => {
  it("disables skills for substrate sandbox agents", () => {
    expect(skillsSupportedForSandboxPlatform(true, "substrate")).toBe(false);
    expect(skillsSupportedForSandboxPlatform(true, "agent-sandbox")).toBe(true);
    expect(skillsSupportedForSandboxPlatform(false, "substrate")).toBe(true);
  });
});
