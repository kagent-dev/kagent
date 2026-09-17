import { useEffect, useState } from "react";

/**
 * Whether the window is narrower than a width, told only when it *crosses* it.
 *
 * The chat has a panel on each side of the transcript, and on a narrow window they
 * leave the conversation squeezed between them. Closing them as the window shrinks is
 * what the application sidebar already does through antd's `breakpoint`, and this is
 * the same behaviour for the two panels antd is not drawing.
 *
 * Crossings rather than a continuous reading, for the reason antd's `onCollapse` gives:
 * a reader who opens a panel back up at a width where it auto-closed should keep it
 * open, and a value read on every render would shut it again on the next resize event.
 * Widening past the width opens it again, which is the point — the space is back.
 *
 * `matchMedia` rather than a resize listener: it fires on the crossing and nowhere in
 * between, so there is nothing to throttle.
 */
export function useCollapsedBelow(width: number): [boolean, (collapsed: boolean) => void] {
  const [isCollapsed, setCollapsed] = useState(() => isNarrowerThan(width));

  useEffect(() => {
    const query = window.matchMedia(`(max-width: ${width}px)`);
    const onCross = (event: MediaQueryListEvent) => setCollapsed(event.matches);
    query.addEventListener("change", onCross);
    return () => query.removeEventListener("change", onCross);
  }, [width]);

  return [isCollapsed, setCollapsed];
}

/** Guarded for the server-less renders the unit suite does without a window. */
function isNarrowerThan(width: number): boolean {
  return typeof window !== "undefined" && window.matchMedia
    ? window.matchMedia(`(max-width: ${width}px)`).matches
    : false;
}
