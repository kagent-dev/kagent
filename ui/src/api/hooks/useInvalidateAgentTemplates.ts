import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The prefix every agent-template read is keyed under — see `useAgentBuildingBlocks`. */
const TEMPLATE_KEY_PREFIX = "agentTemplates.";

/**
 * Re-reads every agent template on screen, wherever it is being shown.
 *
 * A sweep rather than one `refresh()`: readers are keyed three ways, and the agents list
 * is derived from this read too — an agent *is* a template paired with a harness.
 *
 * Resolves once the re-reads have landed, so a caller can await it before navigating.
 */
export function useInvalidateAgentTemplates(): () => Promise<void> {
  const { mutate } = useSWRConfig();

  return useCallback(async () => {
    await mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        key[0].startsWith(TEMPLATE_KEY_PREFIX),
    );
  }, [mutate]);
}
