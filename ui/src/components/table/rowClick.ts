import { useState } from "react";
import type { MouseEvent } from "react";

/**
 * What handles its own click, and so must not also trigger the row's.
 *
 * One rule rather than a `stopPropagation` per control, so a control added to a row
 * later cannot silently inherit the row's navigation or expansion. Popover and
 * dropdown are here because their content is portalled outside the row's markup in
 * the tree but still inside it for event bubbling.
 */
const INTERACTIVE = "a, button, input, [role='button'], .ant-popover, .ant-dropdown";

/**
 * Runs `activate` on a click that did not land on something interactive.
 *
 * The primitive the two helpers below share, and the reason a row's own link keeps
 * working: the guard sees the `a` and bails, so the link navigates once rather than
 * the row navigating over it. Exported for a clickable row that is not a table row —
 * the agent page's schedules are a `ul` of `li`, which needs the guard but brings its
 * own hover styling.
 */
export function rowClickHandler(
  activate: () => void,
  { enabled = true }: { enabled?: boolean } = {},
): ((event: MouseEvent<HTMLElement>) => void) | undefined {
  if (!enabled) return undefined;
  return (event) => {
    if ((event.target as HTMLElement).closest(INTERACTIVE)) return;
    activate();
  };
}

/**
 * Makes a whole antd table row clickable, for `onRow`.
 *
 * What `activate` does is the caller's choice — navigate somewhere, or toggle the
 * row's own expansion through `useExpandedRows` below. What is not the caller's
 * choice is the guard, which is why this exists: `expandRowByClick` and a hand-rolled
 * row handler both treat every click anywhere on the row as an activation, including
 * the delete button — so asking to delete a server also unfolded its tools behind the
 * confirmation.
 *
 * A row that cannot be activated gets neither the handler nor the class, so it does
 * not light up under the pointer promising something that will not happen.
 */
export function clickableRow(
  activate: () => void,
  options: { enabled?: boolean } = {},
): { className?: string; onClick?: (event: MouseEvent<HTMLElement>) => void } {
  const onClick = rowClickHandler(activate, options);
  // `clickable-table-row` is what `GlobalStyles` hangs hover, pointer and pressed on.
  return onClick ? { className: "clickable-table-row", onClick } : {};
}

/**
 * Which rows are unfolded, for a table whose expansion a page has to drive.
 *
 * Deliberately *not* in the URL, unlike a table's filters: which rows are unfolded is
 * a position in a reading session rather than a description of what is being looked
 * at, and a link that reopened somebody else's expanded rows would be odd.
 *
 * `expandAll` takes the keys rather than reading them from anywhere, because only the
 * caller knows which rows are on screen — a filtered, paginated table's "all" is its
 * current page's matches, not everything it could ever show.
 */
export function useExpandedRows() {
  const [keys, setKeys] = useState<string[]>([]);

  function set(key: string, open: boolean) {
    setKeys((current) =>
      open ? [...current, key] : current.filter((candidate) => candidate !== key),
    );
  }

  return {
    keys,
    set,
    toggle: (key: string) => set(key, !keys.includes(key)),
    expandAll: (visible: string[]) => setKeys(visible),
    collapseAll: () => setKeys([]),
    /** Whether every one of `visible` is already open, for a button that says which it will do. */
    allExpanded: (visible: string[]) =>
      visible.length > 0 && visible.every((key) => keys.includes(key)),
  };
}
