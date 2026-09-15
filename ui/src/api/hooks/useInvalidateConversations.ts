import { useInvalidateKeys } from "./useInvalidateKeys";

const KEYS = ["agentInstances."];

/** Re-reads every conversation on screen, wherever it is being shown. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidateConversations(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
