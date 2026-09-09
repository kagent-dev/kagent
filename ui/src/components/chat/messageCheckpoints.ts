import type { Checkpoint, ChatMessage } from "@/api";

/**
 * Which of the reader's messages a saved boundary sits at.
 *
 * A checkpoint names a *turn*, not a message: `headTaskId` is the task the boundary
 * was taken at, and every message of that turn carries it. The mark belongs on the
 * reader's message in that turn, because that is where the controls are — so the
 * last one of theirs in the turn wins, which for a conversation is the only one.
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
    const anchor = lastReaderMessageOfTask(messages, checkpoint.headTaskId);
    if (anchor) byMessage.set(anchor, checkpoint.id);
  }

  for (const [messageId, checkpointId] of savedHere) {
    if (messages.some((message) => message.id === messageId)) {
      byMessage.set(messageId, checkpointId);
    }
  }

  return byMessage;
}

function lastReaderMessageOfTask(
  messages: readonly ChatMessage[],
  taskId: string,
): string | undefined {
  let found: string | undefined;
  for (const message of messages) {
    if (message.role === "user" && message.taskId === taskId) found = message.id;
  }
  return found;
}
