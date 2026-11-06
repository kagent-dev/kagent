import { Alert, Button } from "antd";
import { canForkFrom, type Checkpoint } from "@/api";

/**
 * Offers Revert for a crashed conversation. Forking stays on the transcript's snapshot
 * lines, so the copy mentions them only when one of them can fork.
 *
 * `isRecovering` and `isReverting` are separate because an admitted recovery keeps its
 * operation through every failed attempt. Only this page's own request disables
 * Revert; a click while another attempt runs joins that attempt.
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
