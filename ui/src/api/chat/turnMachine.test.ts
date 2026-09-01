import { describe, expect, it } from "vitest";
import {
  IDLE_TURN,
  isActive,
  nextTurn,
  turnStateOf,
  type TurnEvent,
  type TurnState,
} from "./turnMachine";

/**
 * The turn state machine.
 *
 * Every case below is a transition that *used* to be possible when the turn was
 * tracked as two strings written from four branches of an event loop, and each one
 * rendered as something untrue rather than as an error: a cancelled turn that went
 * on saying it was working, a finished turn resurrected by a late frame, an
 * indicator flickering between two stages for the whole of an answer. There is no
 * screenshot that catches any of them.
 */

const KEY = "kagent/instance-1";

/** A turn taken through a list of events, for stating a history rather than a step. */
function play(events: TurnEvent[], from: TurnState = IDLE_TURN): TurnState {
  return events.reduce(nextTurn, from);
}

const started: TurnEvent = { type: "start", key: KEY };

describe("nextTurn", () => {
  it("puts a fresh turn in `sending`, which is what lets the reader's message be on screen with nothing back yet", () => {
    const turn = nextTurn(IDLE_TURN, started);
    expect(turn.phase).toBe("sending");
    expect(turn.key).toBe(KEY);
    expect(isActive(turn.phase)).toBe(true);
  });

  it("separates waiting from answering, which the transport spells the same way", () => {
    // Both of these are A2A's `working`. The difference between them is a page
    // that looks stalled and a page that looks alive, so the machine keeps it.
    const waiting = play([started, { type: "status", state: "working" }]);
    const answering = play([
      started,
      { type: "status", state: "working" },
      { type: "content" },
    ]);

    expect(waiting.phase).toBe("working");
    expect(answering.phase).toBe("streaming");
    expect(turnStateOf(waiting.phase)).toBe("working");
    expect(turnStateOf(answering.phase)).toBe("working");
  });

  it("does not drop back to `working` for every status frame of a long answer", () => {
    // The runtime emits a `WORKING` frame alongside each chunk it sends. Assigning
    // the phase from the frame would take the indicator backwards between every
    // word of the reply — a flicker for the whole of the answer.
    const turn = play([
      started,
      { type: "content" },
      { type: "status", state: "working" },
      { type: "status", state: "working" },
    ]);
    expect(turn.phase).toBe("streaming");
  });

  it("returns the same object when nothing changed, so a chatty stream does not re-render the page per frame", () => {
    const streaming = play([started, { type: "content" }]);
    const again = nextTurn(streaming, { type: "content" });
    // Identity, not equality: this is what React's bail-out reads.
    expect(again).toBe(streaming);
  });

  it("keeps a cancellation, even against a `completed` frame that was already in flight", () => {
    // The reader presses stop; the server's answer to the previous frame is on the
    // wire already. Assigning it would report the turn as having finished normally.
    const turn = play([
      started,
      { type: "content" },
      { type: "cancel" },
      { type: "status", state: "completed" },
    ]);
    expect(turn.phase).toBe("canceled");
    expect(turnStateOf(turn.phase)).toBe("canceled");
  });

  it("does not let a late frame resurrect a turn that has ended", () => {
    const turn = play([
      started,
      { type: "cancel" },
      { type: "content" },
      { type: "status", state: "working" },
    ]);
    expect(turn.phase).toBe("canceled");
    expect(isActive(turn.phase)).toBe(false);
  });

  it("keeps the first failure rather than the last word", () => {
    const first = new Error("the runtime went away");
    const turn = play([
      started,
      { type: "error", error: first },
      { type: "error", error: new Error("and again") },
    ]);
    expect(turn.phase).toBe("failed");
    expect(turn.error).toBe(first);
  });

  it("fails a turn that goes quiet even when half the answer is already on screen", () => {
    const turn = play([
      started,
      { type: "content" },
      { type: "timeout", error: new Error("The agent stopped responding after 60s.") },
    ]);
    expect(turn.phase).toBe("failed");
    expect(turn.error?.message).toMatch(/stopped responding/);
  });

  it("settles a stream that ended without saying how it went", () => {
    // The measured cluster behaviour: `WORKING`, then the stream closes. A turn
    // left `working` forever would keep the stop button and the spinner on screen.
    const turn = play([started, { type: "status", state: "working" }, { type: "settle" }]);
    expect(turn.phase).toBe("completed");
  });

  it("leaves an already-finished turn alone when the stream closes", () => {
    const failed = play([started, { type: "error", error: new Error("nope") }]);
    expect(nextTurn(failed, { type: "settle" })).toBe(failed);
  });

  it("ends the turn when the agent asks the reader something, so the composer comes back", () => {
    const turn = play([started, { type: "status", state: "input_required" }]);
    expect(turn.phase).toBe("awaiting_input");
    expect(isActive(turn.phase)).toBe(false);
    expect(turnStateOf(turn.phase)).toBe("input_required");
  });

  it("clears the previous turn's failure when a new one starts", () => {
    // Otherwise a retry opens with the error it was pressed to clear still on
    // screen, which reads as the retry having failed instantly.
    const failed = play([started, { type: "error", error: new Error("nope") }]);
    const retried = nextTurn(failed, started);
    expect(retried.error).toBeUndefined();
    expect(retried.phase).toBe("sending");
  });

  it("carries the task id forward once the transport has named it", () => {
    // It is what `cancel` addresses the RPC with, and it arrives on one frame and
    // not the rest.
    const turn = play([
      started,
      { type: "status", state: "submitted", taskId: "task-9" },
      { type: "status", state: "working" },
    ]);
    expect(turn.taskId).toBe("task-9");
  });
});

describe("turnStateOf", () => {
  it("collapses the machine's extra distinctions back into A2A's vocabulary", () => {
    // The browser suite asserts `data-state="canceled"`, a share link reads this,
    // and a contributed component may too — so the transport's spelling stays
    // available even though nothing renders from it directly any more.
    expect(turnStateOf("idle")).toBe("idle");
    expect(turnStateOf("sending")).toBe("submitted");
    expect(turnStateOf("working")).toBe("working");
    expect(turnStateOf("streaming")).toBe("working");
    expect(turnStateOf("awaiting_input")).toBe("input_required");
    expect(turnStateOf("completed")).toBe("completed");
    expect(turnStateOf("failed")).toBe("failed");
    expect(turnStateOf("canceled")).toBe("canceled");
  });
});
