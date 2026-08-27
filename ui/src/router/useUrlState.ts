import { useCallback, useMemo, useRef } from "react";
import { useSearchParams } from "react-router-dom";

/**
 * List state that lives in the address bar.
 *
 * A search term, a set of filters and a sort are all answers to "what am I looking
 * at" — so they belong in the address, which is the one thing a reader can send to
 * somebody else, bookmark, or get back by reloading. Held in `useState` instead,
 * every one of those loses the view: the link opens the unfiltered page, and a
 * refresh throws away the narrowing that made the page worth reading.
 *
 * The rule the whole file follows: **a value equal to its default is absent from the
 * URL.** So the plain page is `/models`, not `/models?q=&ns=&page=1`, and an address
 * carries only what somebody actually chose. That also makes "is anything active?" a
 * question the URL answers by itself.
 *
 * Two details that are decisions rather than style:
 *
 * - **Writes replace rather than push.** Typing six characters into a search box is
 *   one intention, not six, and pushing each keystroke buries the previous page under
 *   a history stack nobody can back out of. The cost is that Back leaves the page
 *   rather than undoing a filter, which is what the clear-filters control is for.
 * - **Every write goes through one function.** Two `setSearchParams` calls in the same
 *   handler both read the parameters as they were before either ran, so the second
 *   silently discards the first — and "clear everything" and "reset the page number
 *   when the filter changes" are exactly that shape. `useUrlStateWriter` takes all the
 *   changes at once, so there is only ever one navigation.
 */

/** A parameter's new value. `null` — or an empty string, or no entries — removes it. */
export type UrlStateValue = string | readonly string[] | null;

/**
 * A change, either as the value to write or as a function of what is already there.
 *
 * The function form exists because a value computed during render is a snapshot, and
 * two clicks can land before the next one: removing two filter pills in quick
 * succession had each removal compute its result from the same stale list, so the
 * second put back what the first took out and one pill survived — the wrong one.
 * Composing at write time, against the same parameters `pending` already tracks, is
 * what makes "remove this one" mean it whenever it arrives.
 */
export type UrlStateChange =
  | UrlStateValue
  | ((previous: readonly string[]) => UrlStateValue);

/** Shared identity for "no values", so a caller can rely on `===` between renders. */
const NO_VALUES: readonly string[] = Object.freeze([]);

/**
 * Applies a set of parameter changes in one navigation.
 *
 * Parameters not named are left exactly as they are, so a page can own `q` without
 * knowing what else the address carries — a contributed parameter, for instance,
 * survives every filter change.
 */
export function useUrlStateWriter(): (changes: Record<string, UrlStateChange>) => void {
  const [searchParams, setSearchParams] = useSearchParams();
  const current = searchParams.toString();

  /**
   * The last address this hook wrote, and the one it was derived from.
   *
   * Because a write is not visible to the next write until React has re-rendered,
   * and two writes can land before it does. Observed, not theorised: a probe clicked
   * "clear filters" and typed into the search box in the next instant, and the second
   * write — reading the parameters as they were before the first — put the cleared
   * namespace filter straight back. On screen that is a filter that will not clear,
   * and a search that reports no matches for a row that is plainly there.
   *
   * `base` is what makes it safe rather than merely sticky: once the router catches
   * up, `current` no longer matches and the fresh address wins. So this composes
   * writes that genuinely raced and never overrides a navigation from anywhere else.
   */
  const pending = useRef<{ base: string; params: URLSearchParams } | null>(null);

  return useCallback(
    (changes: Record<string, UrlStateChange>) => {
      const from =
        pending.current?.base === current
          ? pending.current.params
          : new URLSearchParams(current);

      const next = new URLSearchParams(from);
      for (const [key, change] of Object.entries(changes)) {
        // Resolved against `from`, not against the render that asked: that is the
        // whole point of the function form.
        const value = typeof change === "function" ? change(from.getAll(key)) : change;
        next.delete(key);
        if (value === null) continue;
        if (typeof value === "string") {
          if (value !== "") next.set(key, value);
          continue;
        }
        // Repeated parameters rather than one joined string: `?ns=a&ns=b` needs no
        // separator, so no value has to be escaped and none can be split by accident.
        // A namespace cannot contain a comma; the next filter this is reused for might.
        for (const entry of value) if (entry !== "") next.append(key, entry);
      }

      pending.current = { base: current, params: next };
      setSearchParams(next, { replace: true });
    },
    [current, setSearchParams],
  );
}

/**
 * One text parameter — a search box, a chosen tab, a mode.
 *
 * `fallback` is both the value read when the parameter is absent and the value that
 * removes it, so the default view has a clean address.
 */
export function useUrlState(
  key: string,
  fallback = "",
): [string, (next: string) => void] {
  const [searchParams] = useSearchParams();
  const write = useUrlStateWriter();
  const value = searchParams.get(key) ?? fallback;

  const set = useCallback(
    (next: string) => write({ [key]: next === fallback ? null : next }),
    [write, key, fallback],
  );

  return [value, set];
}

/**
 * One multi-valued parameter — a set of namespaces, states, creators.
 *
 * Empty means the parameter is absent, which every caller here reads as "no
 * narrowing" rather than "narrowed to nothing".
 *
 * The returned array keeps its identity while the values keep theirs, so a page can
 * use it as a `useMemo` dependency and not re-filter its rows on every render.
 * `getAll` alone cannot do that — it builds a fresh array each time it is called,
 * which is a new dependency on every render whether anything changed or not.
 */
export function useUrlListState(
  key: string,
): [readonly string[], (next: readonly string[]) => void] {
  const [searchParams] = useSearchParams();
  const write = useUrlStateWriter();

  const encoded = JSON.stringify(searchParams.getAll(key));
  const value = useMemo(() => {
    const values = JSON.parse(encoded) as string[];
    return values.length === 0 ? NO_VALUES : values;
  }, [encoded]);

  const set = useCallback(
    (next: readonly string[]) => write({ [key]: next.length === 0 ? null : next }),
    [write, key],
  );

  return [value, set];
}

/**
 * One numeric parameter — a page number.
 *
 * A parameter that is not a number reads as the fallback rather than as `NaN`: the
 * address bar is editable, and `?page=banana` should show the first page rather than
 * an empty table with no explanation.
 */
export function useUrlNumberState(
  key: string,
  fallback: number,
): [number, (next: number) => void] {
  const [searchParams] = useSearchParams();
  const write = useUrlStateWriter();

  const raw = searchParams.get(key);
  const parsed = raw === null ? Number.NaN : Number.parseInt(raw, 10);
  const value = Number.isFinite(parsed) ? parsed : fallback;

  const set = useCallback(
    (next: number) => write({ [key]: next === fallback ? null : String(next) }),
    [write, key, fallback],
  );

  return [value, set];
}

/**
 * Reads several multi-valued parameters at once, with a stable identity.
 *
 * For a caller holding a *set* of filters whose names it only learns at runtime — a
 * filter bar built from definitions — where one `useUrlListState` per filter would
 * mean calling a hook in a loop.
 */
export function useUrlListStates(
  keys: readonly string[],
): Record<string, readonly string[]> {
  const [searchParams] = useSearchParams();

  // Both dependencies are strings for the reason given above: `searchParams` and
  // `keys` are fresh objects on renders where nothing about either changed.
  const search = searchParams.toString();
  const encodedKeys = JSON.stringify(keys);

  return useMemo(() => {
    const params = new URLSearchParams(search);
    const out: Record<string, readonly string[]> = {};
    for (const key of JSON.parse(encodedKeys) as string[]) {
      const values = params.getAll(key);
      out[key] = values.length === 0 ? NO_VALUES : values;
    }
    return out;
  }, [search, encodedKeys]);
}
