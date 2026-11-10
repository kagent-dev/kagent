import { act, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ConversationRecap } from "@/api";
import { RECAP_IDLE_MS, useAwayRecap } from "./useAwayRecap";

const RECAP: ConversationRecap = {
  text: "Checkout was crashlooping on a failing liveness probe.",
  turnCount: 3,
  generatedAt: "2026-08-21T10:30:00Z",
};

interface Props {
  recap: ConversationRecap | undefined;
  turnCount: number;
}

let pane: HTMLDivElement;

/** One hook on the shared pane, with the clock the fake timers drive. */
function mount(initial: Props, onAway?: () => void) {
  return renderHook(
    (props: Props) => useAwayRecap({ ...props, target: { current: pane }, now: () => Date.now(), onAway }),
    { initialProps: initial },
  );
}

const wait = (ms: number) => act(() => void vi.advanceTimersByTime(ms));

const INTERACTIONS: Record<string, (el: HTMLElement) => void> = {
  "pointer move": (el) => void fireEvent.pointerMove(el),
  "key press": (el) => void fireEvent.keyDown(el, { key: "a" }),
  scroll: (el) => void fireEvent.scroll(el),
  focusin: (el) => void fireEvent.focusIn(el),
};
const interact = () => act(() => INTERACTIONS["pointer move"](pane));

function setVisibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", { value: state, configurable: true });
  act(() => void document.dispatchEvent(new Event("visibilitychange")));
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
  pane = document.createElement("div");
  document.body.appendChild(pane);
});

afterEach(() => {
  pane.remove();
  setVisibility("visible");
  vi.useRealTimers();
});

describe("useAwayRecap", () => {
  it("is not shown before the reader has been away for three minutes", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS - 1);
    interact();
    expect(result.current.show).toBe(false);
    // Three minutes of silence after that interaction is what turns it on.
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("waits for the first interaction rather than appearing on a timer", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS * 2);
    expect(result.current.show).toBe(false);
    interact();
    expect(result.current.show).toBe(true);
  });

  it.each(Object.keys(INTERACTIONS))("counts %s as coming back", (kind) => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    act(() => INTERACTIONS[kind](pane));
    expect(result.current.show).toBe(true);
  });

  it("does not count an interaction inside the window as an absence", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS - 1000);
    interact();
    wait(1000);
    interact();
    expect(result.current.show).toBe(false);
    // Three minutes of silence after the last interaction is an absence.
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("counts a key press in the composer, beside the transcript, as coming back", () => {
    const composer = document.createElement("textarea");
    pane.appendChild(composer);
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    act(() => void fireEvent.keyDown(composer, { key: "a" }));
    expect(result.current.show).toBe(true);
  });

  it("counts a hidden tab as away", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    setVisibility("hidden");
    wait(RECAP_IDLE_MS);
    setVisibility("visible");
    expect(result.current.show).toBe(false);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("counts a blurred window as away", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    act(() => void window.dispatchEvent(new Event("blur")));
    wait(RECAP_IDLE_MS);
    act(() => void window.dispatchEvent(new Event("focus")));
    expect(result.current.show).toBe(false);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("does not count a short blur as away", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    act(() => void window.dispatchEvent(new Event("blur")));
    wait(RECAP_IDLE_MS - 1000);
    act(() => void window.dispatchEvent(new Event("focus")));
    interact();
    expect(result.current.show).toBe(false);
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("never shows a stale recap, though a current one beside it shows", () => {
    const stale = mount({ recap: RECAP, turnCount: 4 });
    const current = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    expect(current.result.current.show).toBe(true);
    expect(stale.result.current.show).toBe(false);
  });

  it("treats a recap covering more turns than the transcript holds as current", () => {
    const { result } = mount({ recap: { ...RECAP, turnCount: 4 }, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("never shows when there is no recap, though one beside it shows", () => {
    const none = mount({ recap: undefined, turnCount: 3 });
    const some = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    expect(some.result.current.show).toBe(true);
    expect(none.result.current.show).toBe(false);
  });

  it("shows once per absence: further interaction neither hides nor repeats it", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
    wait(60_000);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("is dismissed by a send, and stays dismissed for the rest of that absence", () => {
    const { result, rerender } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);

    // The reader sends, and the runtime writes a recap covering the new turn.
    rerender({ recap: { ...RECAP, turnCount: 4 }, turnCount: 4 });
    interact();
    expect(result.current.show).toBe(false);
  });

  it("shows again after a later absence", () => {
    const { result, rerender } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
    rerender({ recap: { ...RECAP, turnCount: 4 }, turnCount: 4 });
    expect(result.current.show).toBe(false);

    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
  });

  it("hides the recap when it is dismissed", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);

    act(() => result.current.dismiss());
    expect(result.current.show).toBe(false);
  });

  it("stays dismissed through quick interaction, and shows again after a later absence", () => {
    const { result } = mount({ recap: RECAP, turnCount: 3 });
    wait(RECAP_IDLE_MS);
    interact();
    act(() => result.current.dismiss());

    wait(RECAP_IDLE_MS - 1000);
    interact();
    expect(result.current.show).toBe(false);

    wait(RECAP_IDLE_MS);
    interact();
    expect(result.current.show).toBe(true);
  });
});

describe("useAwayRecap onAway", () => {
  it("calls back once when the reader comes back from an absence", () => {
    const onAway = vi.fn();
    mount({ recap: RECAP, turnCount: 3 }, onAway);
    wait(RECAP_IDLE_MS);
    interact();
    interact();
    expect(onAway).toHaveBeenCalledTimes(1);
  });

  it("does not call back on a quick interaction", () => {
    const onAway = vi.fn();
    mount({ recap: RECAP, turnCount: 3 }, onAway);
    wait(RECAP_IDLE_MS - 1000);
    interact();
    expect(onAway).not.toHaveBeenCalled();
  });

  it("calls back again after a later absence", () => {
    const onAway = vi.fn();
    mount({ recap: RECAP, turnCount: 3 }, onAway);
    wait(RECAP_IDLE_MS);
    interact();
    wait(RECAP_IDLE_MS);
    interact();
    expect(onAway).toHaveBeenCalledTimes(2);
  });
});
