import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The prefix every conversation read is keyed under — see `useAgentInstances`. */
const CONVERSATION_KEY_PREFIX = "agentInstances.";

/**
 * Re-reads every conversation on screen, wherever it is being shown.
 *
 * A conversation is rendered by more than one thing at once. The chat page reads the
 * open one on its own (`agentInstances.get`) and the rail beside it reads the list
 * (`agentInstances.list`), so a write that refreshed only the read it was started from
 * left the other showing the old value: renaming from the details modal refreshed the
 * modal and not the rail, and renaming from the rail refreshed the rail and not the
 * modal. Both were correct about their own read and both looked broken.
 *
 * Keyed rather than plumbed. The alternative was handing every surface a callback that
 * refreshes every other surface's read, which is a wiring problem that grows with each
 * new place a conversation appears — and the dashboard would have been the third. SWR
 * already knows who is reading what, so this asks it to revalidate anything keyed as a
 * conversation and lets each caller drop its own refresh.
 *
 * Resolves once the re-reads have landed, so a caller can await it before saying the
 * write succeeded.
 */
export function useInvalidateConversations(): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        key[0].startsWith(CONVERSATION_KEY_PREFIX),
    );
  }, [mutate]);
}
