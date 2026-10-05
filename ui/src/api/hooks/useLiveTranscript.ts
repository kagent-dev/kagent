import { useEffect, useRef } from "react";

/** How often a conversation someone else can write to is re-read. */
const POLL_MS = 4000;

/**
 * Keeps persisted conversation state up to date while it is on screen.
 *
 * A recovered turn and work started in another tab have no local stream. Re-read
 * their task state and transcript until they finish or need human input.
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
    let reading = false;
    const tick = () => {
      if (reading || document.visibilityState !== "visible") return;
      // A slow read must finish before the next poll begins. Otherwise a backend
      // taking longer than POLL_MS would continually supersede its own results.
      reading = true;
      void latest.current().finally(() => { reading = false; });
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
