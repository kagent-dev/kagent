import { useState } from "react";
import { Input, Modal, Typography } from "antd";
import { SubmitError } from "@/components/common/SubmitError";
import { FormField } from "@/components/form/FormField";
import { toastSaved } from "@/components/form/saveToast";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { useTheme } from "@emotion/react";
import {
  apiClient,
  conversationNameProblem,
  MAX_CONVERSATION_NAME_LENGTH,
  type Checkpoint,
} from "@/api";

const { Paragraph } = Typography;

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
  const [failure, setFailure] = useState<unknown>();
  const problem = conversationNameProblem(draft);
  const validation = useSubmitValidation(problem ? [{ field: "name", message: problem }] : []);

  async function save() {
    if (isSaving || !validation.attempt()) return;
    setSaving(true);
    setFailure(undefined);
    try {
      const renamed = await apiClient.agentInstances.checkpoints.rename(checkpoint.id, draft);
      // Awaited before the toast, so the line already shows the new name by the time
      // the reader is told it changed.
      await onRenamed(renamed);
      onClose();
      toastSaved("saved", "snapshot name", renamed.name);
    } catch (cause: unknown) {
      setFailure(cause);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open
      title="Name this snapshot"
      okText="Save"
      okButtonProps={{ loading: isSaving }}
      cancelText="Cancel"
      onOk={() => void save()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Paragraph css={{ color: theme.color.textMuted, fontSize: 13 }}>
        A fork taken from this snapshot is given its name. Leave this empty to go back
        to the generated one.
      </Paragraph>
      <FormField label="Snapshot name" required={false} error={validation.errorFor("name")}>
        <Input
          value={draft}
          id="snapshot-rename-field"
          data-testid="snapshot-rename-input"
          autoFocus
          maxLength={MAX_CONVERSATION_NAME_LENGTH}
          showCount
          onChange={(event) => setDraft(event.target.value)}
          onPressEnter={() => void save()}
        />
      </FormField>
      {failure !== undefined ? (
        <SubmitError
          error={failure}
          what="snapshot name"
          outcome="saved"
          onRetry={() => void save()}
          data-testid="snapshot-rename-error"
        />
      ) : null}
    </Modal>
  );
}
