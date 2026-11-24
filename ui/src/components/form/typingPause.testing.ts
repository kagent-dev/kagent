import { act } from "@testing-library/react";
import { vi } from "vitest";
import { TYPING_PAUSE_MS } from "./useSubmitValidation";

/**
 * For tests of messages shown while typing: fake timers that still run with the clock,
 * so userEvent's own delays work. Restore with `vi.useRealTimers()`.
 */
export function useTypingTimers() {
  vi.useFakeTimers({ shouldAdvanceTime: true });
}

/** Lets typing pause, so messages held while typing show. */
export function pauseTyping() {
  act(() => {
    vi.advanceTimersByTime(TYPING_PAUSE_MS);
  });
}
