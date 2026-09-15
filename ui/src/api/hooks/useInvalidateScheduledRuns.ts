import { useInvalidateKeys } from "./useInvalidateKeys";

const KEYS = ["scheduledRuns."];

/** Re-reads every schedule on screen, wherever it is being shown. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidateScheduledRuns(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
