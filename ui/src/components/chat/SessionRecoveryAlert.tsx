import { Alert, Button } from "antd";
import { canForkFrom, type AgentInstanceRecovery, type Checkpoint } from "@/api";

/**
 * The choice a crashed conversation offers its owner.
 *
 * Revert is the only action here. Forking a saved snapshot is already offered on each
 * snapshot's line in the transcript, which stays visible while recovery is needed, so a
 * second fork control here would be two ways to do one thing. The copy points at those
 * lines only when one of them can fork; naming saved snapshots on a conversation with
 * none ready would offer something the page does not show.
 */
export function SessionRecoveryAlert({
  recovery,
  checkpoints,
  isRecovering,
  onRevert,
}: {
  recovery?: AgentInstanceRecovery;
  checkpoints: readonly Checkpoint[];
  isRecovering: boolean;
  onRevert: () => void | Promise<void>;
}) {
  if (recovery?.needed !== true) return null;

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
            loading={isRecovering}
            disabled={isRecovering}
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
