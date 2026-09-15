import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The prefix every harness read is keyed under — see `useAgentBuildingBlocks`. */
const HARNESS_KEY_PREFIX = "harnesses.";

/**
 * Re-reads every harness list on screen, wherever it is being shown.
 *
 * A sweep rather than one `refresh()`: the tab reads `harnesses.listAll` and a scoped
 * caller `harnesses.list`, so refreshing one means reconstructing the other's key.
 *
 * Resolves once the re-reads have landed, so a caller can await it before navigating.
 */
export function useInvalidateHarnesses(): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        key[0].startsWith(HARNESS_KEY_PREFIX),
    );
  }, [mutate]);
}
