import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The prefix every harness read is keyed under — see `useAgentBuildingBlocks`. */
const HARNESS_KEY_PREFIX = "harnesses.";

/**
 * Re-reads every harness list on screen, wherever it is being shown.
 *
 * A key sweep rather than one `refresh()`, because the two readers are keyed
 * differently and a create cannot know which is mounted: the tab reads
 * `["harnesses.listAll", "<every namespace, sorted>"]` while a scoped caller reads
 * `["harnesses.list", namespace]`. Refreshing either by hand from the create page would
 * mean reconstructing the other's key from the namespace list it has not read.
 *
 * Without this the tab landed on a cached list that did not contain the harness just
 * made — intermittently, because SWR's revalidate-on-mount sometimes won the race and
 * sometimes did not, which is the worst version of the bug: it looks like a create that
 * silently failed, and it looks fine on the next try.
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
