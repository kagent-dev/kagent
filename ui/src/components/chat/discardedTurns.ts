import type { AgentInstanceRecovery, ChatMessage } from "@/api";

/**
 * How a message relates to work a runtime recovery discarded.
 *
 * - `message`: a reader message the controller listed as discarded. It did reach the
 *   agent, which may have started answering, so it is not a message that failed to send.
 * - `reply`: anything after that message in the same task. The controller lists reader
 *   messages only, but the agent output that followed one was produced by the runtime
 *   state the recovery threw away, so it is unremembered too.
 */
export type DiscardedMark = "message" | "reply";

/**
 * Marks each message the recovered runtime does not remember, by message id.
 *
 * A message is matched on both its task and message id, because ids are only unique
 * within a task. Marking starts at the first listed message of a task, not at the task:
 * a task can hold answers captured in the snapshot before the crash, and those stay
 * unmarked. This reads transcript order, so it marks a reply correctly only when the
 * transcript places the reply after the message it answers.
 */
export function discardedMessages(
  messages: readonly ChatMessage[],
  recovery: AgentInstanceRecovery | undefined,
): ReadonlyMap<string, DiscardedMark> {
  const marks = new Map<string, DiscardedMark>();
  const failed = recovery?.failedMessages ?? [];
  if (failed.length === 0) return marks;

  const listed = new Set(failed.map((ref) => `${ref.taskId}\u0000${ref.messageId}`));
  const discardedTasks = new Set<string>();
  for (const message of messages) {
    const taskId = message.taskId;
    if (!taskId) continue;
    if (listed.has(`${taskId}\u0000${message.id}`)) {
      marks.set(message.id, "message");
      discardedTasks.add(taskId);
    } else if (discardedTasks.has(taskId)) {
      marks.set(message.id, "reply");
    }
  }
  return marks;
}
