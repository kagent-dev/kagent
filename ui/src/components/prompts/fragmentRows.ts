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
