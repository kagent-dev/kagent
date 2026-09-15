import { useInvalidateKeys } from "./useInvalidateKeys";

const KEYS = ["models.list", "models.get"];

/** Re-reads every model configuration on screen. Not the whole `models.` prefix: the provider
 * catalogue is the form's costliest read and a save cannot change it. See `useInvalidateKeys` for what a sweep reaches. */
export function useInvalidateModels(): () => Promise<void> {
  return useInvalidateKeys((operation) => KEYS.includes(operation));
}
