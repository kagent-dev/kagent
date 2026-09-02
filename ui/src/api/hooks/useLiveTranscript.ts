import { useEffect, useRef } from "react";

/** How often a conversation someone else can write to is re-read. */
const POLL_MS = 4000;

/**
 * Keeps a two-sided conversation up to date while it is on screen.
 *
 * A share link that allows replies makes a conversation something two people write to,
 * and nothing tells either of them when the other has said something: the transcript is
 * read once on mount, so the other side's messages only appeared on a reload. This
 * re-reads it while the tab is being looked at.
 *
 * Polled rather than streamed. The A2A gateway can stream a *task* — that is how a turn
 * in flight is followed — but a message somebody else sends starts a task this page has
 * no id for, so there is nothing to subscribe to until after it exists. Re-reading the
 * task list is the operation that finds one.
 *
 * Paused while a turn is running here, because the local transcript is then ahead of
 * the server's and a merge would be work for nothing, and paused while the tab is
 * hidden, so a conversation left open in a background tab is not a request every few
 * seconds forever.
 */
export function useLiveTranscript(
  refresh: () => Promise<void>,
  { enabled, isBusy }: { enabled: boolean; isBusy: boolean },
): void {
  // Held in a ref so a new function identity does not tear down the interval and
  // restart the clock, which at this cadence would mean it rarely fired at all.
  const latest = useRef(refresh);
  useEffect(() => {
    latest.current = refresh;
  });

  useEffect(() => {
    if (!enabled || isBusy) return;
    const tick = () => {
      if (document.visibilityState !== "visible") return;
      void latest.current();
    };
    const timer = window.setInterval(tick, POLL_MS);
    // And once on becoming visible again, so returning to the tab does not wait out
    // the rest of an interval before showing what arrived while it was hidden.
    document.addEventListener("visibilitychange", tick);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", tick);
    };
  }, [enabled, isBusy]);
}
