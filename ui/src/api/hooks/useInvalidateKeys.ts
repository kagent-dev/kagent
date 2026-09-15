import { useCallback } from "react";
import { useSWRConfig } from "swr";

/**
 * Re-reads every SWR key whose operation name a caller claims, wherever it is mounted.
 *
 * The shared half of the seven `useInvalidate…` hooks below, which differ only in which
 * operations they name. A sweep rather than one `refresh()`, because a resource is read
 * under several keys — a list, a scoped list, a get — and a writer would otherwise have
 * to reconstruct each from state it has not read.
 *
 * **It reaches what is on screen, and only that.** SWR returns the cached value
 * untouched for a key nothing is subscribed to, so a page that sweeps and then navigates
 * has not refreshed where it is going: that list re-reads on its own mount.
 *
 * Resolves once the re-reads have landed, so a caller can await it before navigating.
 */
export function useInvalidateKeys(names: (name: string) => boolean): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) => Array.isArray(key) && typeof key[0] === "string" && names(key[0]),
    );
    // `names` is a module constant at every call site, so it is stable by construction.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mutate]);
}

/** Every key under one operation prefix — `"prompts."` and the like. */
export function underPrefix(prefix: string): (name: string) => boolean {
  return (name) => name.startsWith(prefix);
}
