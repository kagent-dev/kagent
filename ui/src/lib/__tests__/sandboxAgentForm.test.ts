import {
  buildSandboxConfigFromForm,
  defaultSandboxPlatform,
  sandboxFieldsFromApiSpec,
} from "@/lib/sandboxAgentForm";
import type { AgentFormData } from "@/components/AgentsProvider";

describe("sandboxFieldsFromApiSpec", () => {
  it("maps substrate sandbox spec to form fields", () => {
    expect(
      sandboxFieldsFromApiSpec({
        platform: "substrate",
        substrate: {
          workerPoolRef: { name: "pool-a" },
          snapshotsConfig: { location: "gs://bucket/snapshots" },
        },
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

describe("buildSandboxConfigFromForm", () => {
  const base: AgentFormData = {
    name: "demo",
    namespace: "default",
    description: "d",
    tools: [],
  };

  it("omits sandbox when platform is agent-sandbox", () => {
    expect(buildSandboxConfigFromForm({ ...base, sandboxPlatform: "agent-sandbox" })).toBeUndefined();
  });

  it("builds substrate sandbox config from form fields", () => {
    expect(
      buildSandboxConfigFromForm({
        ...base,
        sandboxPlatform: "substrate",
        substrateWorkerPoolRefName: " wp ",
        substrateSnapshotsLocation: " gs://snap ",
      }),
    ).toEqual({
      platform: "substrate",
      substrate: {
        workerPoolRef: { name: "wp" },
        snapshotsConfig: { location: "gs://snap" },
      },
    });
  });

  it("includes empty substrate object when optional fields are unset", () => {
    expect(buildSandboxConfigFromForm({ ...base, sandboxPlatform: "substrate" })).toEqual({
      platform: "substrate",
      substrate: {},
    });
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
