/**
 * One agent turn, as a state machine.
 *
 * ## Why this is a module rather than four `useState` calls
 *
 * A turn used to be tracked as a state string beside a phase string, each written
 * from whichever branch of the event loop happened to run — and the result was a
 * conversation that could report contradictory things at once: a completed turn
 * still streaming, a cancelled turn overwritten a moment later by a `working`
 * status that was already in flight when the reader pressed stop. Neither is
 * visible in a screenshot; both are visible here, because a transition that must
 * not happen is a branch this file does not have.
 *
 * ## The rule that shapes it
 *
 * **A turn never goes backwards.** The transport is a stream, and a stream can
 * deliver a status frame *after* the content it describes: the runtime emits
 * `WORKING` frames for the whole of a reply, so a naive assignment would drop the
 * turn from "answering" back to "working" between every chunk, and the indicator
 * would flicker for the entire answer. So progress is monotonic within a turn, and
 * only `start` — a new turn — returns to the beginning.
 *
 * ## What it deliberately does not know
 *
 * Anything about the agent *instance*. Resuming and suspending belong to the
 * instance's own lifecycle, which the controller reports on the record rather than
 * in the A2A stream; folding them in here would mean inventing stages the wire
 * does not carry. Nothing joins the two any more: the indicator that did was
 * removed once the server suspended instances itself, and a reader has no
 * decision to make about a lifecycle they no longer drive.
 */

import type { ChatTurnState } from "./types";

/**
 * Where a turn is.
 *
 * Distinct from `ChatTurnState`, which is the *transport's* vocabulary — the A2A
 * task states, one of which (`submitted`) the reader never sees and one of which
 * (`working`) covers both "asked, nothing back" and "the answer is arriving". This
 * is the UI's vocabulary, and it separates those two because they are the
 * difference between a page that looks stalled and a page that looks alive.
 */
export type ChatTurnPhase =
  /** No turn. Either none has been sent, or the last one is long finished. */
  | "idle"
  /** The reader's message is on screen and nothing has come back yet. */
  | "sending"
  /** The agent acknowledged the turn and is working on it. */
  | "working"
  /** Content is arriving — the first message or delta of the answer has landed. */
  | "streaming"
  /** The agent asked the reader something and is waiting. */
  | "awaiting_input"
  | "completed"
  | "failed"
  | "canceled";

/** A turn, and whose conversation it belongs to. */
export interface TurnState {
  /**
   * The conversation this turn is in, as `conversationKey` builds it.
   *
   * Carried on the state rather than beside it because a turn outlives the render
   * that started it: switching conversations mid-turn must leave the new one's
   * transcript idle rather than showing the previous one's progress under it.
   */
  key?: string;
  phase: ChatTurnPhase;
  /** The A2A task, once the transport has named it. Needed to cancel. */
  taskId?: string;
  /** Set only in `failed`. */
  error?: Error;
}

/** What can happen to a turn. */
export type TurnEvent =
  /** The reader sent something. The only event that starts a turn over. */
  | { type: "start"; key: string }
  /** The transport reported the task's state. */
  | { type: "status"; state: ChatTurnState; taskId?: string }
  /** A message or a delta arrived — the answer is visibly happening. */
  | { type: "content" }
  | { type: "error"; error: Error }
  /** The reader pressed stop. */
  | { type: "cancel" }
  /** The stream went quiet for longer than the deployment allows. */
  | { type: "timeout"; error: Error }
  /**
   * The stream ended.
   *
   * Not a terminal state of its own: a transport that closes after a `completed`
   * status has already said how the turn went, and one that closes without saying
   * anything has still finished. So this settles an *active* turn and leaves a
   * finished one exactly as it was.
   */
  | { type: "settle" };

export const IDLE_TURN: TurnState = { phase: "idle" };

/** Whether a turn is still going, which is what the composer and the stop button read. */
export function isActive(phase: ChatTurnPhase): boolean {
  return phase === "sending" || phase === "working" || phase === "streaming";
}

/**
 * Whether a phase is one the turn cannot leave except by starting another.
 *
 * `awaiting_input` counts: the agent has stopped and will not continue on its own,
 * so the composer must come back. It is a *pause* in the conversation and an end of
 * the turn, which is the distinction that makes it belong here.
 */
function isSettled(phase: ChatTurnPhase): boolean {
  return !isActive(phase) && phase !== "idle";
}

/** How far through a turn each phase is. Progress within a turn is monotonic. */
const RANK: Record<ChatTurnPhase, number> = {
  idle: 0,
  sending: 1,
  working: 2,
  streaming: 3,
  awaiting_input: 4,
  completed: 4,
  failed: 4,
  canceled: 4,
};

/** What a transport-reported task state means for the phase. */
const PHASE_BY_TURN_STATE: Record<ChatTurnState, ChatTurnPhase> = {
  submitted: "sending",
  working: "working",
  input_required: "awaiting_input",
  completed: "completed",
  failed: "failed",
  canceled: "canceled",
};

/**
 * The turn after `event`.
 *
 * Pure, and returns the *same object* when nothing changed, so a caller storing
 * this in React state does not re-render for every `working` frame of a long
 * answer — which on a chatty runtime is hundreds of them.
 */
export function nextTurn(current: TurnState, event: TurnEvent): TurnState {
  switch (event.type) {
    case "start":
      // The only reset. Everything the previous turn recorded goes with it,
      // including its error — a retry that kept the last failure on screen would
      // read as the retry having failed too.
      return { key: event.key, phase: "sending" };

    case "status": {
      const wanted = PHASE_BY_TURN_STATE[event.state];
      const taskId = event.taskId ?? current.taskId;
      const phase = advance(current.phase, wanted);
      if (phase === current.phase && taskId === current.taskId) return current;
      return { ...current, phase, taskId };
    }

    case "content":
      // Content is proof the turn is alive, so it can carry a turn forward out of
      // `sending` or `working` — but it must not resurrect a turn that has already
      // ended, which a late frame after a cancel would otherwise do.
      if (isSettled(current.phase) || current.phase === "idle") return current;
      if (current.phase === "streaming") return current;
      return { ...current, phase: "streaming" };

    case "error":
      if (isSettled(current.phase)) return current;
      return { ...current, phase: "failed", error: event.error };

    case "cancel":
      // Allowed from a settled turn as well: the reader pressing stop on a turn
      // that finished a moment ago should still leave the page saying "cancelled"
      // rather than silently doing nothing.
      if (current.phase === "idle") return current;
      return { ...current, phase: "canceled", error: undefined };

    case "timeout":
      // A stream that stopped emitting is a failed turn however far it got —
      // including from `streaming`, where half an answer is on screen and nothing
      // is coming to finish it.
      if (isSettled(current.phase)) return current;
      return { ...current, phase: "failed", error: event.error };

    case "settle":
      if (!isActive(current.phase)) return current;
      return { ...current, phase: "completed" };
  }
}

/**
 * The later of two phases, so a status frame can never take a turn backwards.
 *
 * The one exception is between the four end states, which all share a rank: the
 * first one to arrive is the one that stands, because a `completed` frame already
 * in flight when the reader pressed stop must not overwrite "cancelled".
 */
function advance(current: ChatTurnPhase, wanted: ChatTurnPhase): ChatTurnPhase {
  if (isSettled(current)) return current;
  return RANK[wanted] > RANK[current] ? wanted : current;
}

/**
 * The turn in the transport's own vocabulary.
 *
 * Kept because it is what a share link, a contributed component and the existing
 * browser tests read — `data-state="canceled"` is an assertion in the suite. The
 * two extra distinctions this machine draws collapse back here: `sending` is the
 * task's `submitted`, and `streaming` is still `working` as far as A2A is
 * concerned.
 */
export function turnStateOf(phase: ChatTurnPhase): ChatTurnState | "idle" {
  switch (phase) {
    case "idle":
      return "idle";
    case "sending":
      return "submitted";
    case "working":
    case "streaming":
      return "working";
    case "awaiting_input":
      return "input_required";
    default:
      return phase;
  }
}
