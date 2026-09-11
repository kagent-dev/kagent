import type { Checkpoint, ChatMessage } from "@/api";

/**
 * Which messages a saved boundary covers.
 *
 * A checkpoint names a *turn*, not a message: `headTaskId` is the task the boundary
 * was taken at, and both the question and the answer carry it. So the whole turn is
 * marked — what the reader saved is the exchange, and marking half of it would say
 * the agent's reply is on the other side of a boundary it is not.
 *
 * Two sources, merged, and both are needed:
 *
 * - the controller's list, which is what survives a reload, and
 * - what this page has saved since it loaded, keyed by message id, because the
 *   message the reader has just sent has no task id yet — it is the optimistic copy
 *   put on screen the moment they pressed send, and it gains a task id only when the
 *   transcript is read back. Without the second source a checkpoint would appear to
 *   do nothing until the page was reloaded.
 */
export function checkpointsByMessage(
  messages: readonly ChatMessage[],
  checkpoints: readonly Checkpoint[] | undefined,
  savedHere: ReadonlyMap<string, string>,
): Map<string, string> {
  const byMessage = new Map<string, string>();

  for (const checkpoint of checkpoints ?? []) {
    if (checkpoint.state !== "ready" || !checkpoint.headTaskId) continue;
    for (const message of messages) {
      if (message.taskId === checkpoint.headTaskId) byMessage.set(message.id, checkpoint.id);
    }
  }

  for (const [messageId, checkpointId] of savedHere) {
    const from = messages.findIndex((message) => message.id === messageId);
    if (from === -1) continue;
    // Forward to the next thing the reader says: everything between is the reply to
    // the message they saved, and belongs inside the same boundary.
    for (let at = from; at < messages.length; at += 1) {
      if (at > from && messages[at].role === "user") break;
      byMessage.set(messages[at].id, checkpointId);
    }
  }

  return byMessage;
}

/** A run of messages the transcript draws as one thing. */
export interface TranscriptGroup {
  /** The boundary these messages sit inside, when they sit inside one. */
  checkpointId?: string;
  messages: ChatMessage[];
}

/**
 * The transcript split into what is drawn boxed and what is not.
 *
 * Consecutive messages under the same checkpoint become one group, so the panel is
 * drawn once around the turn rather than once around each half of it. Everything else
 * is its own group of one.
 */
export function groupByCheckpoint(
  messages: readonly ChatMessage[],
  byMessage: ReadonlyMap<string, string>,
): TranscriptGroup[] {
  const groups: TranscriptGroup[] = [];

  for (const message of messages) {
    const checkpointId = byMessage.get(message.id);
    const last = groups[groups.length - 1];
    if (checkpointId && last?.checkpointId === checkpointId) {
      last.messages.push(message);
      continue;
    }
    groups.push({ checkpointId, messages: [message] });
  }

  return groups;
}
