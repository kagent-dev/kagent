import { useInvalidateKeys } from "./useInvalidateKeys";

const KEYS = ["mcpServers.list", "tools.list"];

/** Re-reads every MCP server list on screen. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidateMcpServers(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
