import type { TablePaginationConfig, TableProps } from "antd";
import type { SortOrder } from "antd/es/table/interface";
import type { ListView } from "./useListView";

/**
 * Binds an antd table's sort and page to the `ListView` holding them in the URL.
 *
 * Both are *controlled* here rather than left to the table. A table that keeps its
 * own sort forgets it on every reload and cannot be linked to — which is the whole
 * point of putting the state in the address — and the two have to agree, since a
 * changed sort resets the page number.
 *
 * The columns keep their own `sorter` compare functions, so the ordering itself is
 * still the table's work. That is honest on these pages because the read returns the
 * whole list: sorting rows already in the browser sorts all of them. It would not be
 * honest on a server-paged table, which is why the substrate page's actor and worker
 * columns offer no sort at all.
 */

/** The order antd should draw on a column's header, from the URL's answer. */
export function sortOrderFor(view: ListView, columnKey: string): SortOrder | null {
  if (view.sort?.column !== columnKey) return null;
  return view.sort.direction === "asc" ? "ascend" : "descend";
}

/** The pagination config for a list whose rows are all in the browser. */
export function paginationFor(
  view: ListView,
  total: number,
  pageSize: number,
): TablePaginationConfig {
  return {
    current: view.page,
    pageSize,
    total,
    // A fixed size, so the address has one fewer thing in it and two readers
    // following the same link see the same rows. Worth revisiting when a list here
    // is long enough for the choice to matter.
    showSizeChanger: false,
    // The total is the filtered count and is stated as such by the summary beside
    // the controls; repeating a bare number here would invite reading it as the
    // whole collection.
    showTotal: (count, [from, to]) => `${from}–${to} of ${count}`,
    hideOnSinglePage: false,
  };
}

/**
 * The table's `onChange`, routed into the URL.
 *
 * `extra.action` says which control the reader used, which matters: antd calls this
 * for a sort and a page turn alike, and writing both every time would reset the page
 * whenever a page was turned.
 */
export function listTableChange<T>(
  view: ListView,
): NonNullable<TableProps<T>["onChange"]> {
  return (pagination, _filters, sorter, extra) => {
    if (extra.action === "sort") {
      // Single-sort tables only. `sorter` is an array under antd's multi-sort, and
      // taking the first entry keeps this from throwing if a column ever opts in —
      // the URL then carries the primary key, which is the one a reader chose last.
      const active = Array.isArray(sorter) ? sorter[0] : sorter;
      const column = active?.columnKey;

      if (!active?.order || typeof column !== "string") {
        view.setSort(undefined);
        return;
      }
      view.setSort({
        column,
        direction: active.order === "ascend" ? "asc" : "desc",
      });
      return;
    }

    if (extra.action === "paginate" && pagination.current) {
      view.setPage(pagination.current);
    }
  };
}

/** Case- and accent-insensitive text order, with numbers read as numbers. */
export function byText<T>(of: (row: T) => string) {
  return (a: T, b: T) =>
    of(a).localeCompare(of(b), undefined, { numeric: true, sensitivity: "base" });
}

/** Numeric order, for the count columns. */
export function byNumber<T>(of: (row: T) => number) {
  return (a: T, b: T) => of(a) - of(b);
}

/**
 * Whether a row matches a search term, over the fields the row displays.
 *
 * Case-folded on both sides, and a blank term matches everything rather than nothing
 * — an empty search box is not a filter.
 */
export function matchesQuery(query: string, fields: readonly (string | undefined)[]) {
  const needle = query.trim().toLocaleLowerCase();
  if (needle === "") return true;
  return fields
    .filter((field): field is string => Boolean(field))
    .some((field) => field.toLocaleLowerCase().includes(needle));
}
