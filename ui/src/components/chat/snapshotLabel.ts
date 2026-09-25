import type { Checkpoint } from "@/api";

/**
 * What a snapshot is called on screen.
 *
 * Shared by the mark on the transcript and the record it opens, because both hand it to
 * an extension point as `label` — and a contribution mounted at both would otherwise
 * print two different names for one snapshot.
 *
 * The fallback is for the one case the controller sends nothing: the name column is
 * `NOT NULL` and a cleared name is restored to a generated one, so an empty name means
 * a response this client did not expect rather than a boundary nobody has named.
 */
export function snapshotLabel(checkpoint: Checkpoint): string {
  return checkpoint.name || `Snapshot ${checkpoint.id.slice(0, 8)}`;
}
