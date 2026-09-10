/**
 * Removing a run of saved boundaries, one request each.
 *
 * There is no batch RPC — `DeleteCheckpoint` takes one id — so this is a loop, and it
 * is a *sequential* one. Each delete releases a retained snapshot in the substrate, and
 * firing forty of those at a controller at once to save a second of a reader's time is
 * the wrong trade.
 *
 * A failure does not stop the run. Picking twelve rows and having the third refuse
 * should still remove the other eleven; the reader can see what is left and try again.
 * What must not happen is the toast claiming success over a partial run, so the whole
 * thing rejects when anything failed, and the message says how it went.
 */

export interface SnapshotDeletionError extends Error {
  /** How many were removed before, between and after the failures. */
  deleted: number;
  /** Every one that refused, in the order they were tried. */
  failures: { id: string; reason: string }[];
}

/**
 * Deletes each id in turn, and answers with how many went.
 *
 * Rejects with a `SnapshotDeletionError` when any of them refused — including when
 * some succeeded, because a partial run is not a success and a reader told "Deleted
 * 12 snapshots" would not go looking for the two that are still there.
 */
export async function deleteSnapshots(
  ids: readonly string[],
  remove: (id: string) => Promise<void>,
): Promise<number> {
  const failures: { id: string; reason: string }[] = [];
  let deleted = 0;

  for (const id of ids) {
    try {
      await remove(id);
      deleted += 1;
    } catch (cause: unknown) {
      // The console carries the whole error for whoever is debugging it; the toast
      // gets the count and one reason, because it has a line to say it in.
      console.error(`Could not delete snapshot ${id}:`, cause);
      failures.push({ id, reason: cause instanceof Error ? cause.message : String(cause) });
    }
  }

  if (failures.length > 0) {
    const error = new Error(deletionMessage(deleted, failures)) as SnapshotDeletionError;
    error.deleted = deleted;
    error.failures = failures;
    throw error;
  }

  return deleted;
}

/** What the toast says when a run did not come off cleanly. */
function deletionMessage(
  deleted: number,
  failures: readonly { id: string; reason: string }[],
): string {
  const kept = `${failures.length} ${failures.length === 1 ? "snapshot" : "snapshots"}`;
  const gone = deleted > 0 ? `${deleted} deleted, ` : "";
  // One reason, whichever came first: the rest are in the console, and a toast that
  // lists twelve of them is a toast nobody reads.
  return `${gone}${kept} could not be deleted — ${failures[0].reason}`;
}

/** What the toast says when it did. */
export function deletedMessage(deleted: number): string {
  return `Deleted ${deleted} ${deleted === 1 ? "snapshot" : "snapshots"}`;
}
