import { useState } from "react";
import { Alert, Button, Select, Space } from "antd";
import { canForkFrom, type AgentInstanceRecovery, type Checkpoint } from "@/api";

export function SessionRecoveryAlert({
  recovery,
  checkpoints,
  isRecovering,
  onRevert,
  onFork,
}: {
  recovery?: AgentInstanceRecovery;
  checkpoints: readonly Checkpoint[];
  isRecovering: boolean;
  onRevert: () => void | Promise<void>;
  onFork: (checkpointId: string) => void | Promise<void>;
}) {
  const [checkpointId, setCheckpointId] = useState<string>();
  const [isForking, setForking] = useState(false);
  if (recovery?.needed !== true) return null;

  const busy = isRecovering || isForking;
  const ready = checkpoints.filter(canForkFrom);
  const selected = ready.some((checkpoint) => checkpoint.id === checkpointId)
    ? checkpointId
    : undefined;

  async function fork() {
    if (!selected || busy) return;
    setForking(true);
    try {
      await onFork(selected);
    } finally {
      setForking(false);
    }
  }

  return (
    <Alert
      type={isRecovering ? "info" : "warning"}
      showIcon
      title={isRecovering ? "Recovering this conversation" : "The agent runtime crashed"}
      description={(
        <>
          <div>Revert to the last runtime snapshot, or start a new conversation from a saved snapshot. Recovery does not replay messages it did not retain.</div>
          <Space wrap style={{ marginTop: 8, maxWidth: "100%" }}>
            <Button loading={isRecovering} disabled={busy} onClick={() => void onRevert()}>
              Revert to last snapshot
            </Button>
            {ready.length > 0 && (
              <>
                <Select
                  aria-label="Saved snapshot to fork"
                  placeholder="Choose a saved snapshot"
                  disabled={busy}
                  value={selected}
                  onChange={setCheckpointId}
                  options={ready.map((checkpoint) => ({ value: checkpoint.id, label: checkpoint.name }))}
                  style={{ width: 240, maxWidth: "100%" }}
                />
                <Button loading={isForking} disabled={!selected || busy} onClick={() => void fork()}>
                  Fork snapshot
                </Button>
              </>
            )}
          </Space>
        </>
      )}
    />
  );
}
