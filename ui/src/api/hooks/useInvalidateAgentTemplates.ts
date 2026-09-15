import { useCallback } from "react";
import { useSWRConfig } from "swr";

/** The prefix every agent-template read is keyed under — see `useAgentBuildingBlocks`. */
const TEMPLATE_KEY_PREFIX = "agentTemplates.";

/**
 * Re-reads every agent template on screen, wherever it is being shown.
 *
 * A key sweep rather than one `refresh()`: the list reads `agentTemplates.listAll`, a
 * scoped caller `agentTemplates.list` and a details page `agentTemplates.get`, so
 * refreshing one by hand means reconstructing the others' keys. It has to reach the
 * agents list too — an agent *is* a template paired with a harness, so that list is
 * derived from this read.
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
