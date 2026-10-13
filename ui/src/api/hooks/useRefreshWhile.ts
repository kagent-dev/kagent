import { useEffect, useRef } from "react";

/**
 * Calls `refresh` every `intervalMs` while `active`, and not at all otherwise.
 *
 * For a state the server changes on its own, which a page read once at mount would
 * show forever. Driven from a timer rather than SWR's `refreshInterval`, which SWR's
 * deduplication stretches. A tick that would overlap the one still in flight is
 * dropped rather than queued, so a slow backend gets no backlog.
 */
export function useRefreshWhile(active: boolean, refresh: () => Promise<void>, intervalMs: number): void {
  // Held in a ref so a new function identity per render does not reset the clock.
  const refreshRef = useRef(refresh);
  useEffect(() => {
    refreshRef.current = refresh;
  });

  useEffect(() => {
    if (!active) return;
    let inFlight = false;
    const timer = window.setInterval(() => {
      if (inFlight) return;
      inFlight = true;
      void refreshRef
        .current()
        // A failed read is already on screen as the resource's error; the timer goes on.
        .catch(() => {})
        .finally(() => {
          inFlight = false;
        });
    }, intervalMs);
    return () => window.clearInterval(timer);
  }, [active, intervalMs]);
}
