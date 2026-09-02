import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { useUrlListStates, useUrlStateWriter } from "@/router/useUrlState";

/**
 * Everything a list page's controls hold between them, kept in the address bar.
 *
 * One object rather than five separate pieces of state, for two reasons that are the
 * same reason: the pieces are not independent. Changing a filter has to reset the
 * page number — otherwise narrowing a list while reading page four leaves a reader
 * looking at an empty table that is not empty — and clearing the filters has to clear
 * several parameters at once. Both of those are one navigation each, and only a
 * single owner can make them one.
 *
 * `useUrlState` underneath, so all of it survives a reload and travels in a link.
 */

export type SortDirection = "asc" | "desc";

/** The column a table is ordered by, and which way. */
export interface SortState {
  column: string;
  direction: SortDirection;
}

export interface ListView {
  /** The search term, as typed. Empty means no text narrowing. */
  readonly query: string;
  setQuery(next: string): void;

  /** The values chosen for one filter. Empty means *all* — never "none". */
  selected(filterId: string): readonly string[];
  setSelected(filterId: string, next: readonly string[]): void;

  /**
   * Drops one value from a filter, whatever else is selected when it lands.
   *
   * Distinct from `setSelected` with the remainder computed by the caller: that
   * remainder is a snapshot of the render it came from, and two removals in quick
   * succession then disagree about what was there — the second writes back the value
   * the first removed. This says which one to drop and lets the write resolve it.
   */
  deselect(filterId: string, value: string): void;

  /** The order asked for, or `undefined` for the list's own default order. */
  readonly sort?: SortState;
  setSort(next: SortState | undefined): void;

  /** One-based, as the reader sees it. */
  readonly page: number;
  setPage(next: number): void;

  /** Whether anything is narrowing the list — the search term included. */
  readonly isNarrowed: boolean;

  /**
   * Drops the search term and every filter, in one navigation.
   *
   * Not the sort: an order is not a narrowing, nothing is being hidden by it, and it
   * has no pill in the row this control sits in. Clearing it would be clearing
   * something the reader did not ask about.
   */
  clear(): void;
}

/** Parameter names, kept in one place so a page cannot spell one two ways. */
const QUERY_PARAM = "q";
const SORT_PARAM = "sort";
const DIRECTION_PARAM = "dir";
const PAGE_PARAM = "page";

const NO_VALUES: readonly string[] = Object.freeze([]);

/**
 * @param filterIds the parameter name of each filter this page offers, which is also
 * the id its definition carries. Kept stable by the caller — a literal, or a `useMemo`
 * — since it names what `clear()` clears.
 */
export function useListView(filterIds: readonly string[]): ListView {
  const [searchParams] = useSearchParams();
  const write = useUrlStateWriter();
  const values = useUrlListStates(filterIds);

  const query = searchParams.get(QUERY_PARAM) ?? "";
  const sortColumn = searchParams.get(SORT_PARAM);
  const rawPage = Number.parseInt(searchParams.get(PAGE_PARAM) ?? "", 10);
  const page = Number.isFinite(rawPage) && rawPage > 0 ? rawPage : 1;

  const sort = useMemo<SortState | undefined>(
    () =>
      sortColumn
        ? {
            column: sortColumn,
            // Anything other than the one word that means descending is ascending.
            // A hand-edited `?dir=sideways` then reads as the ordinary order rather
            // than as an order nothing can render.
            direction:
              searchParams.get(DIRECTION_PARAM) === "desc" ? "desc" : "asc",
          }
        : undefined,
    [sortColumn, searchParams],
  );

  const isNarrowed =
    query !== "" || filterIds.some((id) => (values[id] ?? NO_VALUES).length > 0);

  const setQuery = useCallback(
    (next: string) => write({ [QUERY_PARAM]: next, [PAGE_PARAM]: null }),
    [write],
  );

  const setSelected = useCallback(
    (filterId: string, next: readonly string[]) =>
      write({ [filterId]: next.length === 0 ? null : next, [PAGE_PARAM]: null }),
    [write],
  );

  const deselect = useCallback(
    (filterId: string, value: string) =>
      write({
        [filterId]: (previous) => previous.filter((entry) => entry !== value),
        [PAGE_PARAM]: null,
      }),
    [write],
  );

  const setSort = useCallback(
    (next: SortState | undefined) =>
      write({
        [SORT_PARAM]: next?.column ?? null,
        // Ascending is the default, so it is not written — an address carries a
        // direction only where the direction was actually chosen.
        [DIRECTION_PARAM]: next?.direction === "desc" ? "desc" : null,
        [PAGE_PARAM]: null,
      }),
    [write],
  );

  const setPage = useCallback(
    (next: number) => write({ [PAGE_PARAM]: next <= 1 ? null : String(next) }),
    [write],
  );

  const clear = useCallback(() => {
    const changes: Record<string, null> = { [QUERY_PARAM]: null, [PAGE_PARAM]: null };
    for (const id of filterIds) changes[id] = null;
    write(changes);
  }, [write, filterIds]);

  const selected = useCallback(
    (filterId: string) => values[filterId] ?? NO_VALUES,
    [values],
  );

  return {
    query,
    setQuery,
    selected,
    setSelected,
    deselect,
    sort,
    setSort,
    page,
    setPage,
    isNarrowed,
    clear,
  };
}
