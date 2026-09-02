/**
 * The row model behind the fragment editor.
 *
 * Split out from the component so the folding and validation rules can be read
 * (and reused by a form that never renders the editor) without dragging JSX
 * along — and so the editor file exports only a component, which is what Fast
 * Refresh needs to hot-reload it.
 */

/**
 * A fragment being edited.
 *
 * Rows carry their own `id` because the key is the thing the user is editing —
 * keying the list on it would remount the input on every keystroke and lose
 * focus, and two rows may briefly share a key while one is being renamed.
 */
export interface FragmentRow {
  id: string;
  key: string;
  value: string;
}

let rowCounter = 0;

export function newFragmentRow(): FragmentRow {
  rowCounter += 1;
  return { id: `fragment-${rowCounter}`, key: "", value: "" };
}

/**
 * Folds rows into the `data` map the API takes.
 *
 * Rows with a blank key are dropped: an untouched trailing row is the normal way
 * the editor looks, not something to reject the whole form over.
 */
export function fragmentsToData(rows: FragmentRow[]): Record<string, string> {
  const data: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key) data[key] = row.value;
  }
  return data;
}

/**
 * The first duplicated key, or `undefined`.
 *
 * Worth catching before submit because the payload is a map: the second value
 * would silently win and the first fragment would vanish without a word.
 */
export function findDuplicateKey(rows: FragmentRow[]): string | undefined {
  const seen = new Set<string>();
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    if (seen.has(key)) return key;
    seen.add(key);
  }
  return undefined;
}

/**
 * The rows that edit an existing library's fragments.
 *
 * Sorted by key, because `data` is a map: it arrives in whatever order the API server
 * serialised it, and an editor whose rows reshuffled between reads would move a
 * fragment out from under the cursor. The detail page reads its fragments in the same
 * order for the same reason.
 *
 * Never empty — a library the API hands back with no fragments still gets somewhere
 * to type, which is the rule the editor itself follows when the last row is removed.
 */
export function rowsFromData(data: Record<string, string>): FragmentRow[] {
  const rows = Object.entries(data)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => ({ ...newFragmentRow(), key, value }));
  return rows.length > 0 ? rows : [newFragmentRow()];
}

/**
 * What stops a set of rows being saved, in the words a form shows.
 *
 * Shared by the create form and the detail page's edit mode because the controller
 * applies one rule to both: `UpdatePromptTemplate` rejects an empty map — "at least
 * one template key is required" — exactly as `CreatePromptTemplate` does. Two copies
 * of that rule are two things to keep in step with it.
 */
export function fragmentIssues(rows: FragmentRow[]): string[] {
  const issues: string[] = [];
  if (Object.keys(fragmentsToData(rows)).length === 0) {
    issues.push("Add at least one fragment key.");
  }
  const duplicate = findDuplicateKey(rows);
  if (duplicate) issues.push(`Two fragments share the key "${duplicate}".`);
  return issues;
}
