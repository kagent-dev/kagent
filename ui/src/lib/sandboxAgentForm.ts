import type { AgentFormData } from "@/components/AgentsProvider";
import type { AgentResponse, SandboxSubstrateSpec } from "@/types";

export function sandboxFieldsFromApiSpec(substrate?: SandboxSubstrateSpec): {
  substrateWorkerPoolRefName: string;
  substrateSnapshotsLocation: string;
} {
  return {
    substrateWorkerPoolRefName: substrate?.workerPoolRef?.name?.trim() ?? "",
    substrateSnapshotsLocation: substrate?.snapshotsConfig?.location?.trim() ?? "",
  };
}

export function buildSandboxSubstrateFromForm(agentFormData: AgentFormData): SandboxSubstrateSpec | undefined {
  if (!agentFormData.runInSandbox) {
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

/**
 * Agent Substrate supports declarative (Python/Go) and BYO agents. AgentHarness has its own
 * substrate runtime and is configured elsewhere.
 */
export function substrateSupportedForAgentType(agentType: string | undefined): boolean {
  return agentType === "Declarative" || agentType === "BYO" || agentType === undefined;
}

/** Sandbox agents run on Agent Substrate with a dedicated actor per chat session. */
export function isSubstrateSandboxAgent(
  agent: Pick<AgentResponse, "workloadMode" | "agent"> | null | undefined
): boolean {
  return agent?.workloadMode === "sandbox";
}

export type SandboxChatMode = "default" | "multi-session";

/** Sidebar chat behavior for sandbox vs deployment agents. */
export function sandboxChatMode(
  agent: Pick<AgentResponse, "workloadMode" | "agent"> | null | undefined
): SandboxChatMode {
  return agent?.workloadMode === "sandbox" ? "multi-session" : "default";
}
