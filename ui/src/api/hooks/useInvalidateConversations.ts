import { underPrefix, useInvalidateKeys } from "./useInvalidateKeys";

/** Re-reads every conversation on screen, wherever it is being shown. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidateConversations(): () => Promise<void> {
  return useInvalidateKeys(underPrefix("agentInstances."));
}
