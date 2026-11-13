import type { ReactNode } from "react";

/** The list row menus an extension may add an item to. */
export const EXTENSION_ROW_MENU_IDS = [
  "app_agents_agentsList_rowMenu",
  "app_agents_agentTemplatesList_rowMenu",
  "app_agents_harnessesList_rowMenu",
] as const;

export type ExtensionRowMenuId = (typeof EXTENSION_ROW_MENU_IDS)[number];

const EXTENSION_ROW_MENU_ID_SET: ReadonlySet<string> = new Set(EXTENSION_ROW_MENU_IDS);

export function isExtensionRowMenuId(value: string): value is ExtensionRowMenuId {
  return EXTENSION_ROW_MENU_ID_SET.has(value);
}

/**
 * The row a contributed action is for. Structural rather than the API's record, so one
 * contribution can serve all three menus.
 */
export interface ExtensionRowContext {
  /** What the row is, as the menu words it: "agent", "agent template", "harness". */
  kind: string;
  namespace: string;
  name: string;
}

/**
 * An item an extension adds to a list row's actions menu.
 *
 * Data rather than a component, so the host renders it as a real menu item: it looks
 * like Edit and Delete beside it, takes part in keyboard navigation, closes the menu
 * once chosen, and is absent for a row that `visible` rules out.
 */
export interface ExtensionRowAction {
  /** Unique within the menu. Also the item's key. */
  id: string;
  menuId: ExtensionRowMenuId;
  label: string;
  icon?: ReactNode;
  /**
   * Whether this row gets the item. Defaults to every row.
   *
   * Called on every render of the row, so keep it cheap; it may read state outside the
   * row. Must be synchronous: a Promise hides the item and logs an error.
   */
  visible?: (row: ExtensionRowContext) => boolean;
  onClick: (row: ExtensionRowContext) => void | Promise<void>;
}

/** Contributions for one menu, in declaration order. */
export function extensionRowActionsForMenu(
  actions: readonly ExtensionRowAction[] | undefined,
  menuId: ExtensionRowMenuId,
): ExtensionRowAction[] {
  return (actions ?? []).filter((action) => action.menuId === menuId);
}

/** Whether `action` is offered for `row`. One that throws is left out, not the whole table. */
export function isRowActionVisible(action: ExtensionRowAction, row: ExtensionRowContext): boolean {
  try {
    if (!action.visible) return true;
    const result: unknown = action.visible(row);
    // An `async` callback returns a Promise, which is always truthy: it would show the
    // item on every row. Hide it and say why. Any other truthy value counts as true.
    if (result instanceof Promise) {
      console.error(`Extension row action "${action.id}": visible must be synchronous, got a Promise`);
      return false;
    }
    return Boolean(result);
  } catch (error) {
    console.error(`Extension row action "${action.id}" crashed deciding visibility`, error);
    return false;
  }
}

/** Runs `action` for `row`, containing a failure, sync or async, so a click never breaks the page. */
export function runRowAction(action: ExtensionRowAction, row: ExtensionRowContext): void {
  const report = (error: unknown) => console.error(`Extension row action "${action.id}" failed`, error);
  try {
    void Promise.resolve(action.onClick(row)).catch(report);
  } catch (error) {
    report(error);
  }
}
