/**
 * The shared shape every data hook returns.
 *
 * SWR's own result leaves each caller to work out what "loading", "failed" and
 * "there is genuinely nothing here" mean, and they get it slightly differently
 * every time — an empty list rendered as a spinner, an error rendered as an
 * empty state. This wraps it once so all four states are named.
 */

import useSWR, { unstable_serialize, useSWRConfig, type Cache, type SWRConfiguration } from "swr";
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
  const { cache, mutate } = useSWRConfig();
  const resourceKey = unstable_serialize(key);
  const { data, error, isLoading, isValidating } = useSWR<T, ApiError>(
    key,
    () => readResource(cache, resourceKey, fetcher),
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
     * Revalidation updates SWR's shared error/loading state; mutating with a
     * rejected fetch promise does not. SWR resolves even a failed revalidation,
     * so read the completed result from this provider's cache and rethrow it.
     * The render's `error` would still describe the previous read here.
     */
    refresh: async () => {
      await mutate(key);
      const failure = cache.get(resourceKey)?.error;
      if (failure) throw failure;
    },
  };
}

interface ResourceReads {
  latest: Promise<unknown>;
  pending: number;
}

// Share ordering across readers and automatic revalidation on the same provider.
// Entries exist only while reads are in flight, including superseded requests.
const readsByCache = new WeakMap<object, Map<string, ResourceReads>>();

function readResource<T>(cache: Cache<T>, key: string, fetcher: () => Promise<T>): Promise<T> {
  let reads = readsByCache.get(cache);
  if (!reads) {
    reads = new Map();
    readsByCache.set(cache, reads);
  }
  const active = reads;
  const dataBefore = cache.get(key)?.data;
  const request: Promise<T> = Promise.resolve().then(fetcher).catch((cause: unknown) => {
    if (active.get(key)?.latest !== request || cache.get(key)?.data !== dataBefore) {
      // SWR fences obsolete successes, but can still publish obsolete errors.
      // Resolve through the cache so its request/mutation fence discards this
      // result too. Only the current read may publish a failure or retry it.
      return cache.get(key)?.data as T;
    }
    throw cause;
  });
  const previous = active.get(key);
  active.set(key, { latest: request, pending: (previous?.pending ?? 0) + 1 });
  const finish = () => {
    const current = active.get(key);
    if (current && --current.pending === 0) {
      active.delete(key);
      if (active.size === 0) readsByCache.delete(cache);
    }
  };
  void request.then(finish, finish);
  return request;
}

function isEmptyResult(data: unknown): boolean {
  if (data === undefined || data === null) return false; // Not loaded is not empty.
  if (Array.isArray(data)) return data.length === 0;
  if (typeof data === "object") return Object.keys(data).length === 0;
  return false;
}
