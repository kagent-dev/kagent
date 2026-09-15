import { useInvalidateKeys } from "./useInvalidateKeys";

const KEYS = ["agentTemplates."];

/** Re-reads every agent template on screen — an agent is a template paired with a harness, so the agents list is derived from this read. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidateAgentTemplates(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
