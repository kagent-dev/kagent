/**
 * A saved turn boundary in a conversation, which a fork can start from.
 *
 * `CheckpointService` takes no cutoff: a checkpoint is always the conversation's
 * *latest* boundary at the moment it is taken. Anchoring one to a message earlier in
 * the transcript is therefore not something the reader chooses at fork time — it is
 * something they had to have saved while that message was the newest.
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
