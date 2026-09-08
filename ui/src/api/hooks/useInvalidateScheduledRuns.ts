import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The prefix every scheduled-run read is keyed under — see `ScheduledRunsPage`. */
const SCHEDULED_RUN_KEY_PREFIX = "scheduledRuns.";

/**
 * Re-reads every schedule on screen, wherever it is being shown.
 *
 * A key sweep rather than one `refresh()`, because the same schedule is read under
 * several keys: the list carries its page token, the agent page's section reads the
 * first hundred under a key of its own, and the detail and history reads are keyed per
 * schedule. Deleting one and refreshing only the list left the agent page still
 * offering it.
 *
 * Resolves once the re-reads have landed, so a caller can navigate afterwards without
 * the list rendering the row it just removed.
 */
export function useInvalidateScheduledRuns(): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        key[0].startsWith(SCHEDULED_RUN_KEY_PREFIX),
    );
  }, [mutate]);
}
