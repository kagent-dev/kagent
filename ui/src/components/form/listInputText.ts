/**
 * How typed text splits into items. "spaces" splits on whitespace, and quotes keep a
 * part with spaces together, as in a shell. "commasAndSpaces" splits on both.
 */
export type ListInputSplit = "spaces" | "commasAndSpaces";

interface Scan {
  /** Items the text closed with a separator. */
  items: string[];
  /** The unfinished item at the end: as typed, and with its quotes removed. */
  trailing?: { raw: string; value: string };
}

/** Splits typed text into finished items and the one still being typed. */
export function scanListText(text: string, split: ListInputSplit): Scan {
  const items: string[] = [];
  const isSeparator = (char: string) => /\s/.test(char) || (split === "commasAndSpaces" && char === ",");
  let value = "";
  let start: number | undefined;
  let quote: string | undefined;
  for (let at = 0; at < text.length; at++) {
    const char = text[at];
    if (quote) {
      if (char === quote) quote = undefined;
      else value += char;
    } else if (isSeparator(char)) {
      if (start !== undefined) items.push(value);
      value = "";
      start = undefined;
    } else {
      start ??= at;
      if (split === "spaces" && (char === '"' || char === "'")) quote = char;
      else value += char;
    }
  }
  return { items: items.filter(Boolean), trailing: start === undefined ? undefined : { raw: text.slice(start), value } };
}
