import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The reads a registered server changes, as `useMcpServers` keys them. */
const SERVER_KEYS = ["mcpServers.list", "tools.list"];

/**
 * Re-reads every MCP server list on screen.
 *
 * A key sweep rather than `refresh()`, so a writer does not have to subscribe to a list
 * it never renders just to have something to refresh.
 *
 * Resolves once the re-reads have landed, so a caller can await it before navigating.
 */
export function useInvalidateMcpServers(): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        SERVER_KEYS.includes(key[0]),
    );
  }, [mutate]);
}
