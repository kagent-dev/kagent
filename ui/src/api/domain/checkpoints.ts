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

/** One page of saved boundaries, and how many the filter matched in all. */
export interface CheckpointPage {
  checkpoints: Checkpoint[];
  /** Everything the filter matched, not just this page. */
  total: number;
}

/** A column a listing can be ordered by, and which way. */
export interface CheckpointSort {
  field: "createdAt" | "conversation" | "state";
  descending?: boolean;
}

export interface Checkpoint {
  id: string;
  agentInstanceId: string;
  /**
   * What the conversation was called when this was taken, and the only name a query can
   * filter or order by. Empty for a conversation that never had one.
   */
  conversationName: string;
  /** The turn this boundary sits at. Matches `taskId` on that turn's messages. */
  headTaskId: string;
  state: CheckpointState;
  /** RFC3339. */
  createdAt?: string;
  /** Why it failed, when it did. */
  failure?: string;
}
