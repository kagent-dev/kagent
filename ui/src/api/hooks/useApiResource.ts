/**
 * The shared shape every data hook returns.
 *
 * SWR's own result leaves each caller to work out what "loading", "failed" and
 * "there is genuinely nothing here" mean, and they get it slightly differently
 * every time — an empty list rendered as a spinner, an error rendered as an
 * empty state. This wraps it once so all four states are named.
 */

import useSWR, { type SWRConfiguration } from "swr";
import { ApiError } from "../ApiError";

export interface ApiResource<T> {
  data: T | undefined;
  /** First load, nothing to show yet. False on a background revalidation. */
  isLoading: boolean;
  /** A request is in flight, including a refresh over data already on screen. */
  isValidating: boolean;
  error: ApiError | undefined;
  /** Loaded successfully and there is nothing in it. */
  isEmpty: boolean;
  /** Refetches and resolves once the new data has landed. */
  refresh: () => Promise<void>;
}

/**
 * Runs `fetcher` under SWR and reports the result in `ApiResource` terms.
 *
 * @param key a stable SWR key, or `null` to hold the request back (the
 * conditional-fetch pattern for a hook whose argument is not ready yet).
 */
export function useApiResource<T>(
  key: readonly unknown[] | null,
  fetcher: () => Promise<T>,
  config?: SWRConfiguration<T, ApiError>,
): ApiResource<T> {
  const { data, error, isLoading, isValidating, mutate } = useSWR<T, ApiError>(
    key,
    fetcher,
    config,
  );

  return {
    data,
    // A held-back request (`key === null`) is idle, not loading — otherwise a
    // page waiting on a route param renders a spinner that never resolves.
    isLoading: key !== null && isLoading,
    isValidating,
    error: error ?? undefined,
    isEmpty: !isLoading && !error && isEmptyResult(data),
    /**
     * Refetches, and rejects when the refetch failed.
     *
     * The bare `mutate()` this used to be does not reject: SWR captures a failed
     * revalidation into `error` and resolves anyway, so a caller awaiting it cannot
     * tell a refresh that worked from one that did not. That is fine for a
     * background revalidation and wrong for a button someone pressed — it made the
     * Refresh control report success over a page showing a load error.
     *
     * Handing `mutate` the promise instead makes it the new value: SWR records the
     * rejection in `error` as before, so the page still shows what went wrong, and
     * rethrows it here so the caller knows too.
     */
    refresh: async () => {
      await mutate(fetcher(), { revalidate: false });
    },
  };
}

function isEmptyResult(data: unknown): boolean {
  if (data === undefined || data === null) return false; // Not loaded is not empty.
  if (Array.isArray(data)) return data.length === 0;
  if (typeof data === "object") return Object.keys(data).length === 0;
  return false;
}
