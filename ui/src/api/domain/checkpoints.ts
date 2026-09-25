/**
 * A saved turn boundary in a conversation, which a fork can start from.
 *
 * Creation names the terminal task the reader intends to save. That task must
 * still be the latest boundary; if the conversation advances, creation fails.
 * A pending snapshot may be retried using the same request and task IDs.
 *
 * `headTaskId` is what ties a checkpoint back to the transcript: it names the turn the
 * boundary sits at, and every message of that turn carries the same id. That is the
 * only durable link between the two, so it is what the chat marks messages from.
 */

/**
 * How far the controller has got with retaining the snapshot.
 *
 * `CheckpointState` in the proto, spelled as words for the reasons given in
 * `agentInstances.ts`. Creation answers `ready` or `failed` and never `creating` —
 * the RPC copies the snapshot before it returns — so a caller that wants to fork can
 * check the state it was handed rather than poll.
 */
export type CheckpointState =
  | "unspecified"
  | "creating"
  | "ready"
  | "failed"
  | "deleting"
  | "unknown";

export interface Checkpoint {
  id: string;
  agentInstanceId: string;
  /**
   * What the reader calls it, and what a fork taken from it is named.
   *
   * Never empty: the controller generates one at creation and puts that default back
   * when a name is cleared, so a fork always has something to be called.
   */
  name: string;
  /** The turn this boundary sits at. Matches `taskId` on that turn's messages. */
  headTaskId: string;
  state: CheckpointState;
  /** RFC3339. */
  createdAt?: string;
  /** Why it failed, when it did. */
  failure?: string;
}

/** Whether a fork can start from this checkpoint. */
export function canForkFrom(checkpoint: Checkpoint): boolean {
  return checkpoint.state === "ready";
}

