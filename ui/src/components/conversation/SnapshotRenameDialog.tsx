import { useState } from "react";
import { Input, Modal, Typography } from "antd";
import { useTheme } from "@emotion/react";
import toast from "react-hot-toast";
import {
  apiClient,
  conversationNameProblem,
  MAX_CONVERSATION_NAME_LENGTH,
  type Checkpoint,
} from "@/api";

const { Paragraph, Text } = Typography;

/**
 * Names a snapshot, or puts the generated name back.
 *
 * Validated with `conversationNameProblem` rather than a second copy of the same
 * rules: the controller enforces one limit on both fields, so two validators here
 * would be two chances to disagree with it. Surrounding whitespace is refused rather
 * than trimmed, and empty is valid — it is how a generated name is restored.
 *
 * Mounted only while open, which is what seeds the box with the stored name without an
 * effect to put it there — see `RenameConversationDialog`, which this follows.
 */
export function SnapshotRenameDialog({
  checkpoint,
  onClose,
  onRenamed,
}: {
  checkpoint: Checkpoint;
  onClose: () => void;
  /**
   * The record as it stands after a rename.
   *
   * Handed over rather than left to a re-read: the caller owns which snapshot this is
   * for, and this is the one moment that record changes under it.
   */
  onRenamed: (renamed: Checkpoint) => void | Promise<void>;
}) {
  const theme = useTheme();
  const [draft, setDraft] = useState(checkpoint.name);
  const [isSaving, setSaving] = useState(false);
  const problem = conversationNameProblem(draft);

  async function save() {
    if (problem) return;
    setSaving(true);
    try {
      const renamed = await apiClient.agentInstances.checkpoints.rename(checkpoint.id, draft);
      // Awaited before the toast, so the line already shows the new name by the time
      // the reader is told it changed.
      await onRenamed(renamed);
      onClose();
      toast.success(`Snapshot renamed to “${renamed.name}”`);
    } catch (cause: unknown) {
      // Deliberately not transient: the old name is still on screen, and a reader who
      // missed the message would believe it changed.
      toast.error(
        `Could not rename the snapshot: ${
          cause instanceof Error ? cause.message : String(cause)
        }`,
        { duration: Infinity },
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open
      title="Name this snapshot"
      okText="Save"
      okButtonProps={{ loading: isSaving, disabled: problem !== undefined }}
      cancelText="Cancel"
      onOk={() => void save()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Paragraph css={{ color: theme.color.textMuted, fontSize: 13 }}>
        A fork taken from this snapshot is given its name. Leave this empty to go back
        to the generated one.
      </Paragraph>
      {/* The id is on a wrapper this app owns: antd spreads unknown props onto its
          inner `<input>`, which is not somewhere a test can reason about. */}
      <div data-testid="snapshot-rename-input">
        <Input
          value={draft}
          autoFocus
          maxLength={MAX_CONVERSATION_NAME_LENGTH}
          showCount
          status={problem ? "error" : undefined}
          onChange={(event) => setDraft(event.target.value)}
          onPressEnter={() => void save()}
          aria-label="Snapshot name"
        />
      </div>
      {problem ? (
        <Text
          data-testid="snapshot-rename-problem"
          css={{ color: theme.color.dangerText, fontSize: 12 }}
        >
          {problem}
        </Text>
      ) : null}
    </Modal>
  );
}
