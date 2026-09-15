import { underPrefix, useInvalidateKeys } from "./useInvalidateKeys";

/** Re-reads every harness list on screen, wherever it is being shown. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidateHarnesses(): () => Promise<void> {
  return useInvalidateKeys(underPrefix("harnesses."));
}
