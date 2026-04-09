import type { AgentType } from "@/types";

/** Sandbox create flow + non-empty image → BYO-shaped sandbox workload. */
export function isSandboxByoImageChosen(
  agentType: AgentType,
  byoImage: string | undefined | null,
): boolean {
  return agentType === "Sandbox" && !!byoImage?.trim();
}

/** Model, tools, prompts (and related) sections. */
export function formUsesDeclarativeSections(
  agentType: AgentType,
  byoImage: string | undefined | null,
): boolean {
  return agentType === "Declarative" || (agentType === "Sandbox" && !isSandboxByoImageChosen(agentType, byoImage));
}

/** Container image and deployment-style fields. */
export function formUsesByoSections(
  agentType: AgentType,
  byoImage: string | undefined | null,
): boolean {
  return agentType === "BYO" || isSandboxByoImageChosen(agentType, byoImage);
}

/** Create-agent form type from GET /agents response (SandboxAgent → form "Sandbox"). */
export function formAgentTypeFromApi(
  specType: AgentType,
  runInSandbox: boolean | undefined,
): AgentType {
  return runInSandbox ? "Sandbox" : specType;
}
