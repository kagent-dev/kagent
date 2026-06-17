import type { AgentType } from "@/types";

/** Declarative vs BYO on the standard Agent / SandboxAgent form (not OpenClaw harness). */
export type AgentFormWorkloadKind = Extract<AgentType, "Declarative" | "BYO">;

/** Model, tools, prompts (and related) sections. */
export function formUsesDeclarativeSections(agentType: AgentType): boolean {
  if (agentType === "AgentHarness") {
    return false;
  }
  return agentType === "Declarative";
}

/** Container image and deployment-style fields. */
export function formUsesByoSections(agentType: AgentType): boolean {
  if (agentType === "AgentHarness") {
    return false;
  }
  return agentType === "BYO";
}

/** Maps API agent spec type to the create/edit form workload kind. */
export function formWorkloadKindFromApi(specType: AgentType): AgentFormWorkloadKind {
  return specType === "BYO" ? "BYO" : "Declarative";
}
