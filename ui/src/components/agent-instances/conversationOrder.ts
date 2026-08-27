import type { AgentInstance } from "@/api";

/**
 * Newest conversation first.
 *
 * The rail used to render in whatever order `ListAgentInstances` answered in, which is
 * an order in no particular order — so the conversation somebody started a minute ago
 * could sit anywhere in the list.
 *
 * By when it was **started**, and deliberately not by when it was last spoken in, which
 * is the more useful thing and is not available here. `AgentInstance.updatedAt` looks
 * like the field for it and is not: the `agent_instance` table carries no timestamp
 * columns at all, the two on the message come out of the row's serialized blob, and
 * `UpdatedAt` is written in exactly two places — when the instance is created and when
 * it goes `CREATING` -> `READY`. Sending a message never touches it, so ordering by it
 * would be creation order under a label claiming otherwise.
 *
 * The signal that *would* answer it is `agent_instance_task.updated_at`, bumped on every
 * task upsert — on the task table, and not returned by `ListAgentInstances`. Reaching it
 * from here costs one `ListTasks` per row, which is why `useConversationTitles` budgets
 * thirty of them for titles alone. When the read grows a last-activity timestamp, this
 * comparator is the one thing that needs to change.
 *
 * Ties break on the id so that equal timestamps give one fixed order rather than
 * whatever the sort happened to do with them — a rail that reshuffles equal rows between
 * reads is the defect this is meant to remove.
 */
export function byNewestFirst(left: AgentInstance, right: AgentInstance): number {
  const when = right.createdAt.localeCompare(left.createdAt);
  return when !== 0 ? when : left.id.localeCompare(right.id);
}
