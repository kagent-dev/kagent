import type { AgentFormData } from "@/components/AgentsProvider";
import type { SandboxConfig, SandboxPlatform } from "@/types";

export function sandboxFieldsFromApiSpec(sandbox?: SandboxConfig): {
  sandboxPlatform: SandboxPlatform;
  substrateWorkerPoolRefName: string;
  substrateSnapshotsLocation: string;
} {
  return {
    sandboxPlatform: sandbox?.platform === "substrate" ? "substrate" : "agent-sandbox",
    substrateWorkerPoolRefName: sandbox?.substrate?.workerPoolRef?.name?.trim() ?? "",
    substrateSnapshotsLocation: sandbox?.substrate?.snapshotsConfig?.location?.trim() ?? "",
  };
}

export function buildSandboxConfigFromForm(agentFormData: AgentFormData): SandboxConfig | undefined {
  if (agentFormData.sandboxPlatform !== "substrate") {
    return undefined;
  }

  const substrate: NonNullable<SandboxConfig["substrate"]> = {};
  const wp = agentFormData.substrateWorkerPoolRefName?.trim();
  if (wp) {
    substrate.workerPoolRef = { name: wp };
  }
  const loc = agentFormData.substrateSnapshotsLocation?.trim();
  if (loc) {
    substrate.snapshotsConfig = { location: loc };
  }

  return {
    platform: "substrate",
    substrate,
  };
}

/** Default sandbox platform for new agents when substrate is available on the cluster. */
export function defaultSandboxPlatform(substrateEnabled: boolean): SandboxPlatform {
  return substrateEnabled ? "substrate" : "agent-sandbox";
}
