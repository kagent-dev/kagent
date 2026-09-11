import { useEffect, useState } from "react";

/**
 * A value that follows its input, but only once it has stopped changing.
 *
 * For anything a keystroke feeds into a *request*: without it a five-letter search is
 * five reads, four of them already stale by the time they land.
 */
export function useDebounced<T>(value: T, delayMs: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [value, delayMs]);
  return settled;
}

/** Long enough to swallow a typed word, short enough not to feel held back. */
export const FILTER_DEBOUNCE_MS = 300;
