/**
 * What is happening in this conversation, read from what the controller actually says.
 *
 * Two different nouns, and keeping them apart is the point. An `AgentInstance` is a
 * **conversation** — one exchange with an agent, which is itself a template paired
 * with a harness. So the lifecycle operations below (resume, suspend) are things
 * happening to the *conversation*, while a turn in flight is the *agent* working. A
 * reading that called an instance an agent would contradict every list page.
 *
 * Nothing draws this any more. The indicator that did was removed once the server
 * suspended instances itself, and a reader has no decision to make about a
 * lifecycle they no longer drive. What survives is the one question the chat page
 * still has to answer — whether to watch the instance closely — which is a join of
 * two records, each reporting a different half of the same moment.
 *
 * ## Every stage is observed, and none is invented
 *
 * The three stages come from two places, both stated outright by the controller:
 *
 * - **`AgentInstance.operation`** — the lifecycle operation claimed *right now*.
 *   The workflow claims one before it acts and clears it when it finishes
 *   (`claim`/`finish` in `go/core/v2/agentinstance/workflow.go`), so a non-
 *   `unspecified` value means that operation is in flight this moment. `resume`
 *   and `suspend` are two of its five values.
 * - **The turn's own phase**, from `useChat`'s state machine, which is what the
 *   A2A stream reported about the task.
 *
 * `AgentInstance.state` supplies the resting reading between turns.
 *
 * ## What cannot be shown, and is therefore not shown
 *
 * **A runtime that suspends itself after answering is visible here only if the
 * controller claims a `suspend` operation on the record for it.** Nothing else in
 * the API reports a self-suspend: an instance has no event stream, and an A2A task
 * says nothing about the actor behind it. So the suspending stage lights up for a
 * suspend the controller is carrying out and stays dark otherwise, rather than
 * being animated on a timer after every answer — which would be a picture of
 * something this build cannot see.
 *
 * The same goes for how far through a resume is: the record says an operation is
 * in flight, not its progress. A progress bar here would be invented.
 */

import type { AgentInstance, ChatTurnPhase } from "@/api";

/** The stages an agent passes through, in order. */
export type LifecycleStage = "resuming" | "running" | "suspending";

/** What the indicator says, and which stage — if any — is happening now. */
export interface LifecycleReading {
  /** The stage under way, or `undefined` when the agent is between stages. */
  active?: LifecycleStage;
  /** The whole reading in one line, for the label and the tooltip. */
  summary: string;
  /** Where the reading came from, so the tooltip can say rather than imply. */
  source: string;
}

/**
 * What the instance and the turn together say the agent is doing.
 *
 * Ordered by which observation is the most specific rather than by which is
 * newest: a claimed lifecycle operation outranks everything, because the
 * controller is telling us it is acting on the instance this moment, and a turn's
 * own state cannot contradict that. Below it comes the turn, and below that the
 * instance's resting state.
 *
 * Pure, and exported, because this is the part worth testing — the rendering is
 * three dots.
 */
export function lifecycleReading(
  instance: AgentInstance | undefined,
  turnPhase: ChatTurnPhase,
  /**
   * Whether the conversation is holding a question the agent asked.
   *
   * Passed in rather than inferred from the phase, because it outlives the turn:
   * after a reload the turn is `idle` and the conversation is still parked, and a
   * reading of "Ready" would be wrong about an agent that will refuse the next
   * thing the reader types.
   */
  isAwaitingReply = false,
): LifecycleReading {
  if (!instance) {
    return {
      summary: "Conversation not read yet",
      source:
        "The conversation's record has not loaded, so nothing about it can be said.",
    };
  }

  if (instance.operation === "resume") {
    return {
      active: "resuming",
      summary: "Resuming this conversation",
      source: "The controller has claimed a resume operation on this conversation.",
    };
  }

  if (instance.operation === "suspend") {
    return {
      active: "suspending",
      summary: "Suspending this conversation",
      source: "The controller has claimed a suspend operation on this conversation.",
    };
  }

  if (instance.operation === "create") {
    return {
      active: "resuming",
      summary: "Preparing this conversation",
      source: "The controller has claimed a create operation on this conversation.",
    };
  }

  if (instance.state === "creating") {
    return {
      active: "resuming",
      summary: "Preparing this conversation",
      source: "The conversation reports its state as creating.",
    };
  }

  if (isAwaitingReply) {
    return {
      summary: "Waiting for your answer",
      source:
        "The agent's last turn is parked in its input-required state. It holds the instance's one active-task slot, so nothing further is accepted until it is given up.",
    };
  }

  switch (turnPhase) {
    case "sending":
      return {
        active: "running",
        summary: "Sent — waiting for the agent",
        source: "The message has gone and the A2A stream has not answered yet.",
      };
    case "working":
      return {
        active: "running",
        summary: "The agent is working",
        source: "The A2A task reports itself as working.",
      };
    case "streaming":
      return {
        active: "running",
        summary: "The agent is answering",
        source: "Content is arriving on the A2A stream.",
      };
    case "awaiting_input":
      return {
        summary: "Waiting for you",
        source: "The A2A task is in its input-required state.",
      };
    default:
      break;
  }

  if (instance.state === "suspended") {
    return {
      // "Agent is suspended", like the ready reading: beside a transcript a bare state
      // reads as a verdict on the last answer rather than as the agent's own condition.
      summary: "Agent is suspended",
      source:
        "The conversation reports its state as suspended. It is resumed when it is next asked something.",
    };
  }

  if (instance.state === "failed") {
    return {
      summary: "Failed",
      source:
        instance.failure?.message || "The conversation reports its state as failed.",
    };
  }

  if (instance.state === "ready") {
    return {
      // "Agent is ready" rather than "Ready", so the word has a subject. Beside a
      // transcript, a bare state reads as a verdict on the last answer.
      summary: "Agent is ready",
      source: "The conversation is ready and no operation is in flight.",
    };
  }

  return {
    summary: `Agent is ${instance.state}`,
    source: "Read from the instance's own state; no lifecycle operation is in flight.",
  };
}

/**
 * Whether the page should be watching the instance closely.
 *
 * Exported so the page can drive its own refresh from it rather than deciding
 * separately and drifting: the moments worth polling are exactly the moments this
 * component has something changing to show.
 */
export function isLifecycleBusy(
  instance: AgentInstance | undefined,
  turnPhase: ChatTurnPhase,
  isAwaitingReply = false,
): boolean {
  return lifecycleReading(instance, turnPhase, isAwaitingReply).active !== undefined;
}
