import { useCallback } from "react";
import { useSWRConfig } from "swr";

/**
 * The reads that are *configurations*, as `useModels` keys them. Not the whole `models.`
 * prefix: the provider catalogue is the form's costliest read and a save cannot change it.
 */
const CONFIGURATION_KEYS = ["models.list", "models.get"];

/**
 * Re-reads every model configuration on screen, wherever it is being shown.
 *
 * A key sweep rather than `refresh()`, so a writer does not have to subscribe to a list
 * it never renders just to have something to refresh.
 *
 * Resolves once the re-reads have landed, so a caller can await it before navigating.
 */
export function useInvalidateModels(): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        CONFIGURATION_KEYS.includes(key[0]),
    );
  }, [mutate]);
}
