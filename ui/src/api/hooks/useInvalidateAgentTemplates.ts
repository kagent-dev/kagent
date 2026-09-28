import { useInvalidateKeys } from "./useInvalidateKeys";

const KEYS = ["agentTemplates."];


export function useInvalidateAgentTemplates(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
