import { Alert, Button } from "antd";
import { canForkFrom, type Checkpoint } from "@/api";

/**
 * The choice a crashed conversation offers its owner.
 *
 * Revert is the only action here. Forking a saved snapshot is already offered on each
 * snapshot's line in the transcript, which stays visible while recovery is needed, so a
 * second fork control here would be two ways to do one thing. The copy points at those
 * lines only when one of them can fork; naming saved snapshots on a conversation with
 * none ready would offer something the page does not show.
 *
 * An admitted recovery can fail and stay pending until a client retries it, so the alert
 * keeps both choices rather than only showing progress. A revert joins the recovery
 * already admitted instead of starting a second one, and a fork reads only a saved
 * snapshot, so neither races an attempt from another tab or client.
 *
 * That is why `isRecovering` and `isReverting` are separate. An admitted recovery keeps
 * its operation until a revert succeeds, so it outlives every failed attempt. Disabling
 * Revert for as long as one was admitted left the button spinning after the first
 * attempt failed, which is the one moment the reader needs it. Only this
 * page's own request in flight disables it; a click while another attempt runs joins
 * that attempt, and one refused because other lifecycle work holds the runtime comes
 * back as an error the reader can retry.
 */
export function SessionRecoveryAlert({
  needed,
  checkpoints,
  isRecovering,
  isReverting,
  onRevert,
}: {
  needed?: boolean;
  checkpoints: readonly Checkpoint[];
  /** A recovery is admitted for this conversation, by anyone, or this page is reverting. */
  isRecovering: boolean;
  /** This page's own revert request is in flight. */
  isReverting: boolean;
  onRevert: () => void | Promise<void>;
}) {
  if (needed !== true) return null;

  const canFork = checkpoints.some(canForkFrom);

  return (
    <Alert
      type={isRecovering ? "info" : "warning"}
      showIcon
      title={isRecovering ? "Recovering this conversation" : "The agent runtime crashed"}
      description={(
        <>
          <div>
            Revert to the last runtime snapshot to continue this conversation.
            {canFork ? " To start a new conversation instead, fork a saved snapshot from the transcript." : null}
            {" "}The reverted agent does not remember messages sent after that snapshot.
          </div>
          <Button
            loading={isReverting}
            disabled={isReverting}
            onClick={() => void onRevert()}
            style={{ marginTop: 8 }}
          >
            Revert to last snapshot
          </Button>
        </>
      )}
    />
  );
}
