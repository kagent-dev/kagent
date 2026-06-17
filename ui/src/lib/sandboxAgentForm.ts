import type { AgentFormData } from "@/components/AgentsProvider";
import type { AgentResponse, SandboxPlatform, SandboxSubstrateSpec } from "@/types";

export function sandboxFieldsFromApiSpec(platform?: SandboxPlatform, substrate?: SandboxSubstrateSpec): {
  sandboxPlatform: SandboxPlatform;
  substrateWorkerPoolRefName: string;
  substrateSnapshotsLocation: string;
} {
  return {
    sandboxPlatform: platform === "substrate" ? "substrate" : "agent-sandbox",
    substrateWorkerPoolRefName: substrate?.workerPoolRef?.name?.trim() ?? "",
    substrateSnapshotsLocation: substrate?.snapshotsConfig?.location?.trim() ?? "",
  };
}

export function buildSandboxSubstrateFromForm(agentFormData: AgentFormData): SandboxSubstrateSpec | undefined {
  if (agentFormData.sandboxPlatform !== "substrate") {
    return undefined;
  }

  const substrate: SandboxSubstrateSpec = {};
  const wp = agentFormData.substrateWorkerPoolRefName?.trim();
  if (wp) {
    substrate.workerPoolRef = { name: wp };
  }
  const loc = agentFormData.substrateSnapshotsLocation?.trim();
  if (loc) {
    substrate.snapshotsConfig = { location: loc };
  }

  return substrate;
}

export function buildSandboxPlatformFromForm(agentFormData: AgentFormData): SandboxPlatform | undefined {
  return agentFormData.sandboxPlatform === "substrate" ? "substrate" : undefined;
}

/** Default sandbox platform for new agents when substrate is available on the cluster. */
export function defaultSandboxPlatform(substrateEnabled: boolean): SandboxPlatform {
  return substrateEnabled ? "substrate" : "agent-sandbox";
}

/**
 * Agent Substrate supports declarative (Python/Go) and BYO agents. AgentHarness has its own
 * substrate runtime and is configured elsewhere.
 */
export function substrateSupportedForAgentType(agentType: string | undefined): boolean {
  return agentType === "Declarative" || agentType === "BYO" || agentType === undefined;
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

/**
 * Default ADK runtime for sandbox agents. Substrate supports both Python and Go; it defaults to
 * Go (faster cold start, the original substrate runtime) but the runtime remains selectable.
 */
export function defaultDeclarativeRuntimeForSandboxPlatform(
  sandboxPlatform: SandboxPlatform | undefined
): "go" | "python" {
  return sandboxPlatform === "substrate" ? "go" : "python";
}

/** Skills are not supported on Agent Substrate sandbox agents yet. */
export function skillsSupportedForSandboxPlatform(
  runInSandbox: boolean,
  sandboxPlatform: SandboxPlatform
): boolean {
  return !(runInSandbox && sandboxPlatform === "substrate");
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
