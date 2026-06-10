import type { AgentFormData } from "@/components/AgentsProvider";
import type { AgentResponse, SandboxConfig, SandboxPlatform } from "@/types";

export function sandboxFieldsFromApiSpec(platform?: SandboxPlatform, sandbox?: SandboxConfig): {
  sandboxPlatform: SandboxPlatform;
  substrateWorkerPoolRefName: string;
  substrateSnapshotsLocation: string;
} {
  return {
    sandboxPlatform: platform === "substrate" ? "substrate" : "agent-sandbox",
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
    substrate,
  };
}

export function buildSandboxPlatformFromForm(agentFormData: AgentFormData): SandboxPlatform | undefined {
  return agentFormData.sandboxPlatform === "substrate" ? "substrate" : undefined;
}

/** Default sandbox platform for new agents when substrate is available on the cluster. */
export function defaultSandboxPlatform(substrateEnabled: boolean): SandboxPlatform {
  return substrateEnabled ? "substrate" : "agent-sandbox";
}

/** Substrate sandbox agents get a dedicated actor per chat session. */
export function isSubstrateSandboxAgent(
  agent: Pick<AgentResponse, "workloadMode" | "agent"> | null | undefined
): boolean {
  return (
    agent?.workloadMode === "sandbox" &&
    agent?.agent?.spec?.platform === "substrate"
  );
}

/** Classic agent-sandbox workloads keep one persistent chat session. */
export function isSingleSessionSandboxAgent(
  agent: Pick<AgentResponse, "workloadMode" | "agent"> | null | undefined
): boolean {
  return agent?.workloadMode === "sandbox" && !isSubstrateSandboxAgent(agent);
}

/** Default ADK runtime for sandbox agents. Substrate uses Go only. */
export function defaultDeclarativeRuntimeForSandboxPlatform(
  sandboxPlatform: SandboxPlatform | undefined
): "go" | "python" {
  return sandboxPlatform === "substrate" ? "go" : "python";
}

/** Skills are not supported on Agent Substrate sandbox agents yet. */
export function skillsSupportedForSandboxPlatform(
  agentType: string,
  sandboxPlatform: SandboxPlatform
): boolean {
  return !(agentType === "Sandbox" && sandboxPlatform === "substrate");
}

export type SandboxChatMode = "default" | "single-session" | "multi-session";

/** Sidebar chat behavior for sandbox vs deployment agents. */
export function sandboxChatMode(
  agent: Pick<AgentResponse, "workloadMode" | "agent"> | null | undefined
): SandboxChatMode {
  if (agent?.workloadMode !== "sandbox") {
    return "default";
  }
  if (isSubstrateSandboxAgent(agent)) {
    return "multi-session";
  }
  return "single-session";
}
