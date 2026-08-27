import type { ReactNode } from "react";

/**
 * Tables an extension may contribute a column to.
 *
 * Named like the extension points, minus the slot segment — the table itself is
 * the target.
 */
export const VENDOR_TABLE_IDS = [
  "app_agents_agentsList_table",
  "app_models_modelsList_table",
  "app_mcpServers_mcpServersList_table",
  "app_prompts_promptsList_table",
] as const;

export type VendorTableId = (typeof VENDOR_TABLE_IDS)[number];

const VENDOR_TABLE_ID_SET: ReadonlySet<string> = new Set(VENDOR_TABLE_IDS);

export function isVendorTableId(value: string): value is VendorTableId {
  return VENDOR_TABLE_ID_SET.has(value);
}

/**
 * A column an extension adds to one of the application's tables.
 *
 * A component slot cannot express this. A slot occupies a position in the DOM,
 * whereas a column is a heading, a per-row renderer and a place in an ordering —
 * three things that have to be declared together for a table to lay out at all.
 * Contributing one is how a product whose domain is wider than the
 * application's — an installation spanning several clusters, say — shows that
 * extra dimension on a page the application still owns.
 *
 * `TRow` is the row type of the table named by `tableId`, so a contribution
 * reads the real record rather than being handed something stringly-typed.
 */
export interface VendorTableColumn<TRow = never> {
  /** Unique within the table. Also the React key. */
  id: string;
  tableId: VendorTableId;
  title: string;
  /**
   * Placed after the core column with this `key`. Falls back to the end when the
   * named column is absent, so a core table can drop a column without an
   * extension's contribution disappearing with it.
   */
  after?: string;
  render: (row: TRow) => ReactNode;
  width?: number | string;
}

/**
 * Declares a column with its row type inferred, so `render` is checked against
 * the real record instead of being widened to `never` at the config site.
 */
export function defineVendorTableColumn<TRow>(
  column: VendorTableColumn<TRow>,
): VendorTableColumn<TRow> {
  return column;
}

/** Contributions for one table, in declaration order. */
export function vendorColumnsForTable<TRow>(
  columns: readonly VendorTableColumn<never>[] | undefined,
  tableId: VendorTableId,
): VendorTableColumn<TRow>[] {
  return (columns ?? []).filter(
    (column) => column.tableId === tableId,
  ) as unknown as VendorTableColumn<TRow>[];
}

/**
 * All this module needs of a core column: something to position against.
 *
 * Only `key` is read — core columns are passed through untouched — so the type
 * asks for nothing else. Describing more would mean restating a table library's
 * column type and disagreeing with it at the edges, which is exactly what
 * happened before this was narrowed.
 */
interface ColumnLike {
  /** As wide as React's own key type: a table library may allow a bigint. */
  key?: string | number | bigint;
}

/**
 * Core columns with an extension's folded in at their requested positions.
 *
 * Pages call this instead of holding a static array, so a contributed column
 * needs no change to the page beyond the one call.
 */
export function withVendorColumns<TRow, TColumn extends ColumnLike>(
  coreColumns: readonly TColumn[],
  contributed: readonly VendorTableColumn<TRow>[],
): TColumn[] {
  if (contributed.length === 0) return [...coreColumns];

  const asColumn = (column: VendorTableColumn<TRow>) =>
    ({
      key: column.id,
      title: column.title,
      width: column.width,
      render: (_value: unknown, row: TRow) => column.render(row),
    }) as unknown as TColumn;

  const result: TColumn[] = [];
  const placed = new Set<string>();

  for (const core of coreColumns) {
    result.push(core);
    for (const column of contributed) {
      if (column.after !== undefined && column.after === core.key) {
        result.push(asColumn(column));
        placed.add(column.id);
      }
    }
  }

  // Anything with no `after`, or naming a column this table does not have.
  for (const column of contributed) {
    if (!placed.has(column.id)) result.push(asColumn(column));
  }

  return result;
}
