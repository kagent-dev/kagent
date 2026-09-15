import { useCallback } from "react";
import { useSWRConfig } from "swr";

/**
 * Re-reads every SWR key whose operation one of `names` claims, wherever it is mounted.
 *
 * The shared half of the seven `useInvalidate…` hooks beside it, which differ only in
 * which operations they name. A sweep rather than one `refresh()`, because a resource is
 * read under several keys — a list, a scoped list, a get — and a writer would otherwise
 * have to reconstruct each from state it has not read.
 *
 * An entry ending in `.` claims every operation under that prefix; anything else names
 * one exactly. Strings rather than a predicate, so `names` can sit in the dependency
 * array and be compared: a callback taking a closure could keep a stable identity only
 * by pinning the first render's, which is a trap for a predicate reading props or state.
 *
 * **It reaches what is on screen, and only that.** SWR returns the cached value
 * untouched for a key nothing is subscribed to, so a page that sweeps and then navigates
 * has not refreshed where it is going: that list re-reads on its own mount.
 *
 * Resolves once the re-reads have landed, so a caller can await it before navigating.
 */
export function useInvalidateKeys(names: readonly string[]): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        names.some((name) =>
          name.endsWith(".") ? key[0].startsWith(name) : key[0] === name,
        ),
    );
  }, [mutate, names]);
}
