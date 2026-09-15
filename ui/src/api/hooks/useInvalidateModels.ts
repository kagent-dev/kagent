import { useCallback } from "react";
import { useSWRConfig } from "swr";

/**
 * The reads that are *configurations*, as `useModels` keys them.
 *
 * Not every `models.` key: `models.providers` and `models.providerModels` are the
 * provider catalogue, which a create cannot change and which is the most expensive read
 * on the form. Sweeping by bare prefix would re-fetch them on every save.
 */
const CONFIGURATION_KEYS = ["models.list", "models.get"];

/**
 * Re-reads every model configuration on screen, wherever it is being shown.
 *
 * A key sweep rather than `useModels().refresh()`, which is what the create page used
 * to do: that subscribes the form to a list it never renders, so opening "New model"
 * issued a read of every configuration purely to have something to call `refresh` on.
 * This asks SWR to revalidate whatever is keyed as a model and subscribes to nothing.
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
