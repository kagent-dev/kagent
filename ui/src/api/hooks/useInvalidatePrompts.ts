import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The prefix every prompt library read is keyed under — see `usePrompts`. */
const PROMPT_KEY_PREFIX = "prompts.";

/**
 * Re-reads every prompt library on screen, wherever it is being shown.
 *
 * A key sweep rather than a pair of `refresh()` calls, because the list's key carries
 * the namespace filter — `["prompts.list", "kagent,platform"]` — so a save that
 * refreshed `usePrompts()` would refresh the unfiltered read and leave whichever
 * filtered one the list actually holds still showing the old key count. The detail
 * read is keyed per library besides. SWR already knows who is reading what, so this
 * asks it to revalidate anything keyed as a prompt library.
 *
 * Resolves once the re-reads have landed, so a caller can await it before saying the
 * write succeeded.
 */
export function useInvalidatePrompts(): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        key[0].startsWith(PROMPT_KEY_PREFIX),
    );
  }, [mutate]);
}
