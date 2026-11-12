import { useEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import type { ConversationRecap } from "@/api";

/** How long the reader must be away from the chat pane before a recap is offered. */
export const RECAP_IDLE_MS = 180_000;

const INTERACTIONS = ["pointermove", "keydown", "scroll", "focusin"] as const;

/** Whether to show the stored recap: current, and the reader has just come back. */
export function useAwayRecap({
  recap,
  turnCount,
  target,
  now = Date.now,
  onAway,
}: {
  recap: ConversationRecap | undefined;
  turnCount: number;
  /** The whole chat pane (transcript and composer), whose pointer, key, scroll and focus events count as being here. */
  target: RefObject<HTMLElement | null>;
  /** Injected so tests control the clock. */
  now?: () => number;
  /** Called when the reader comes back from an absence, so the caller can fetch the latest recap. */
  onAway?: () => void;
}): { show: boolean; collapsed: boolean; toggle: () => void } {
  // The turn count when the reader came back. A later send moves past it, which dismisses.
  const [shownAtTurns, setShownAtTurns] = useState<number | null>(null);
  // The recap the reader folded to its header; a newer recap, or the next absence, opens it again.
  const [collapsedRecap, setCollapsedRecap] = useState<string | null>(null);
  const turnsRef = useRef(turnCount);
  const nowRef = useRef(now);
  const onAwayRef = useRef(onAway);
  useEffect(() => {
    turnsRef.current = turnCount;
    nowRef.current = now;
    onAwayRef.current = onAway;
  });

  // The pane may mount after this hook does (a transcript still loading has none), so
  // each commit checks whether it has appeared or changed.
  const attached = useRef<{ element: HTMLElement; detach: () => void } | null>(null);
  useEffect(() => {
    const element = target.current;
    if (attached.current?.element === element) return;
    attached.current?.detach();
    attached.current = null;
    if (!element) return;
    // A hidden tab or blurred window raises no interactions, so time spent there ages this.
    let lastInteraction = nowRef.current();
    const onInteraction = () => {
      const time = nowRef.current();
      if (time - lastInteraction >= RECAP_IDLE_MS) {
        setShownAtTurns(turnsRef.current);
        setCollapsedRecap(null);
        onAwayRef.current?.();
      }
      lastInteraction = time;
    };
    for (const type of INTERACTIONS) element.addEventListener(type, onInteraction, { passive: true });
    attached.current = {
      element,
      detach: () => {
        for (const type of INTERACTIONS) element.removeEventListener(type, onInteraction);
      },
    };
  });
  useEffect(() => () => {
    attached.current?.detach();
    attached.current = null;
  }, []);

  return {
    show: shownAtTurns === turnCount && recap !== undefined && recap.turnCount >= turnCount,
    collapsed: recap !== undefined && collapsedRecap === recap.generatedAt,
    toggle: () => setCollapsedRecap((current) => (recap === undefined || current === recap.generatedAt ? null : recap.generatedAt)),
  };
}
