import { underPrefix, useInvalidateKeys } from "./useInvalidateKeys";

/** Re-reads every prompt library on screen, wherever it is being shown. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidatePrompts(): () => Promise<void> {
  return useInvalidateKeys(underPrefix("prompts."));
}
