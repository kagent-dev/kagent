import type { AgentCondition } from "@/api";

const STAGES = ["Accepted", "ResolvedRefs", "Compatible", "Ready"] as const;

/** The condition that best explains whether a template/harness pair can run. */
export function pairRevisionCondition(
  conditions: readonly AgentCondition[],
): AgentCondition | undefined {
  for (const type of STAGES) {
    const failure = conditions.find(
      (condition) => condition.type === type && condition.status === "False",
    );
    if (failure) return failure;
  }
  return conditions.find((condition) => condition.type === "Ready");
}
