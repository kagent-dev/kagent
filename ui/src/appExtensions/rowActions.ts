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
 *
 * Give it a `link` to go somewhere in the app (rendered as a real link, so ctrl/middle-click
 * opens a new tab) or an `onClick` to do something else; exactly one of the two.
 */
export type ExtensionRowAction = {
  /** Unique within the menu. Also the item's key. */
  id: string;
  menuId: ExtensionRowMenuId;
  label: string;
  icon?: ReactNode;
  /**
   * Whether this row gets the item. Defaults to every row.
   *
   * Called on every render of the row, so keep it cheap; it may read state outside the
   * row. Must be synchronous: a Promise is truthy, so it would show the item on every row.
   */
  visible?: (row: ExtensionRowContext) => boolean;
} & (
  | {
      /** An in-app route (router `to`) for this row. */
      link: (row: ExtensionRowContext) => string;
      onClick?: undefined;
    }
  | {
      onClick: (row: ExtensionRowContext) => void | Promise<void>;
      link?: undefined;
    }
);

/** Contributions for one menu, in declaration order. */
export function extensionRowActionsForMenu(
  actions: readonly ExtensionRowAction[] | undefined,
  menuId: ExtensionRowMenuId,
): ExtensionRowAction[] {
  return (actions ?? []).filter((action) => action.menuId === menuId);
}

// Visibility and links are computed on every render of every row, so a failure is reported
// once per action and distinct failure, not each time. Held per action object, so nothing
// outlives the action or leaks between tests, and a different error is still reported.
const reported = new WeakMap<ExtensionRowAction, Set<string>>();

function reportOnce(action: ExtensionRowAction, what: string, error: unknown) {
  const seen = reported.get(action) ?? new Set<string>();
  reported.set(action, seen);
  const key = `${what}: ${String(error)}`;
  if (seen.has(key)) return;
  seen.add(key);
  console.error(`Extension row action "${action.id}" ${what}`, error);
}

/** Whether `action` is offered for `row`. One that throws is left out, not the whole table. */
export function isRowActionVisible(action: ExtensionRowAction, row: ExtensionRowContext): boolean {
  try {
    if (!action.visible) return true;
    return action.visible(row);
  } catch (error) {
    reportOnce(action, "crashed deciding visibility", error);
    return false;
  }
}

/** The route a link action points at for `row`, or undefined when it throws or returns no route. */
export function rowActionLink(
  action: Extract<ExtensionRowAction, { link: unknown }>,
  row: ExtensionRowContext,
): string | undefined {
  try {
    const to = action.link(row);
    if (typeof to === "string" && to) return to;
    reportOnce(action, "returned no route from link", to);
  } catch (error) {
    reportOnce(action, "crashed building its link", error);
  }
  return undefined;
}

/** Runs `action` for `row`, containing a failure, sync or async, so a click never breaks the page. */
export function runRowAction(action: Extract<ExtensionRowAction, { onClick: unknown }>, row: ExtensionRowContext): void {
  const report = (error: unknown) => console.error(`Extension row action "${action.id}" failed`, error);
  try {
    void Promise.resolve(action.onClick(row)).catch(report);
  } catch (error) {
    report(error);
  }
}
